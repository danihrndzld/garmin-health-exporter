---
module: electron-app/main
date: 2026-04-16
problem_type: security_issue
component: authentication
severity: high
symptoms:
  - "String.prototype.startsWith(home) matches sibling dirs like /Users/dani-evil/"
  - "A user-supplied outputDir could escape $HOME while passing validation"
  - "A symlink inside $HOME pointing outside $HOME was silently accepted"
root_cause: missing_validation
resolution_type: code_fix
related_components:
  - tooling
tags:
  - path-traversal
  - electron
  - ipc
  - node
  - filesystem
  - defense-in-depth
---

# `startsWith(home)` is not a containment check

## Problem

An Electron IPC handler validated a user-supplied `outputDir` by calling
`resolvedOutput.startsWith(os.homedir())`. The intent was "must be inside the
user's home directory." In practice, `startsWith` is a byte-prefix match on the
string, so any sibling path whose name **starts with** the home-directory string
passed the check. On top of that, a symlink placed inside `$HOME` pointing
outside `$HOME` was accepted because the link itself had a valid home-prefixed
path.

## Symptoms

- Target path `/Users/dani-evil/loot/` passes a check against home
  `/Users/dani`, because `"/Users/dani-evil/..."`.startsWith(`"/Users/dani"`)
  is `true`.
- A symlink at `~/escape -> /etc` is accepted — the link path starts with
  `/Users/dani` even though it resolves outside home.
- The Electron main process then writes arbitrary files to the attacker's
  chosen destination via the validated IPC handler.

## What didn't work

1. **Appending a trailing separator to the home prefix** (`home + path.sep`)
   stops the `dani-evil` case but does nothing about symlinks — the string
   path is still under home, the resolution is not.
2. **Rejecting absolute paths with `path.isAbsolute`** — doesn't help; the
   attack inputs are already absolute and look normal.
3. **Whitelisting by extension/filename** — orthogonal. The bug is *where*,
   not *what*.

## Solution

Two-part containment: use `path.relative` for string containment, and
`fs.realpathSync` for symlink resolution.

```js
// Before — vulnerable
const resolved = path.resolve(outputDir);
if (!resolved.startsWith(os.homedir())) {
  return { ok: false, error: 'Output directory must be within your home directory.' };
}

// After — containment + symlink resolution
let resolved = path.resolve(outputDir);
try {
  if (fs.existsSync(resolved)) {
    resolved = fs.realpathSync(resolved);
  }
} catch (_e) {
  return { ok: false, error: 'Output directory could not be resolved.' };
}
const rel = path.relative(os.homedir(), resolved);
const escapesHome = rel.startsWith('..') || path.isAbsolute(rel);
if (escapesHome) {
  return { ok: false, error: 'Output directory must be within your home directory.' };
}
```

The key move: `path.relative(from, to)` returns a path that starts with `..`
(or is absolute on Windows when `from` and `to` are on different drives) iff
`to` is *not* contained in `from`. That's a structural property of the path,
not a prefix coincidence.

## Why this works

- `path.relative` walks the path component-by-component, so
  `/Users/dani-evil` against `/Users/dani` yields `../dani-evil/...`, which
  trivially fails the `startsWith('..')` guard.
- `fs.realpathSync` resolves every symlink on the path, so an in-home
  symlink pointing to `/etc` becomes `/etc` before the containment check
  runs.
- The `fs.existsSync` gate is necessary because `realpathSync` throws on
  non-existent paths, and we still want to accept a user-supplied path that
  the handler will create. The check treats the string path as
  authoritative when it doesn't exist yet — safe because there are no
  symlinks to worry about along a path that isn't materialised.

## Prevention

- **Rule**: never use `startsWith` to check path containment. Use
  `path.relative` + `..`/absolute guard, or a dedicated library
  (`is-path-inside`) if you want it in one call.
- **Rule**: every cross-boundary filesystem input handler should pass its
  final path through `fs.realpathSync` before deciding to trust it.
- **Electron-specific**: IPC handlers run in the main process with full
  filesystem privileges. Any `outputDir`/`filePath`-shaped argument coming
  from the renderer is adversarial input — treat it exactly like a web
  request body.
- **Test it**: add unit tests that feed in
  `${home}-evil/x`, `${home}/escape` (symlink → `/etc`), and
  `../${path.basename(home)}-evil` and expect rejection. A single positive
  test against a legitimate path inside home is not sufficient coverage.

## References

- Fix commits: `582ab8b` (path.relative), `de1704d` (realpath)
- Node.js `path.relative` docs: the result starts with `..` iff the target
  is not under the base.
- OWASP "Path Traversal" — same class of bug, usually encountered via
  `../` segments; this is the sibling-prefix variant of the same family.
