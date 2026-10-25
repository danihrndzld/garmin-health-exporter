//! MyFitnessPal sign in. The user signs in inside an app window; we copy the session
//! cookies to `mfp_session.json` (mode 0600) for the Go core. The password never
//! reaches Rust or the UI, and the login window has no IPC permissions.
use serde::{Deserialize, Serialize};
use std::{
    fs,
    io::{self, Write},
    os::unix::fs::OpenOptionsExt,
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    time::{SystemTime, UNIX_EPOCH},
};
use tauri::{
    webview::{Cookie, PageLoadEvent},
    AppHandle, Emitter, Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder,
};
use time::{format_description::well_known::Rfc3339, OffsetDateTime};

const LOGIN_URL: &str = "https://www.myfitnesspal.com/account/login";
const LOGIN_PATH: &str = "/account/login";
const LOGIN_WINDOW: &str = "mfp-login";
const SESSION_FILE: &str = "mfp_session.json";
const SESSION_COOKIE: &str = "__Secure-next-auth.session-token";
/// A full Safari string. The Go core sends the same one, because Cloudflare ties
/// `cf_clearance` to the User-Agent.
const USER_AGENT: &str = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Safari/605.1.15";

/// One cookie as the Go core reads it (`internal/mfp/session.go`, `Cookie`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
struct SavedCookie {
    name: String,
    value: String,
    domain: String,
    path: String,
    /// Unix seconds; 0 for a session cookie.
    expires: i64,
}

/// The whole `mfp_session.json`, field for field with the Go core's `Session`. Every field
/// has a default so a file the Go core rewrote with fewer fields still reads.
#[derive(Debug, Default, Serialize, Deserialize)]
#[serde(default)]
struct SessionFile {
    cookies: Vec<SavedCookie>,
    user_agent: String,
    saved_at: Option<String>,
    session_expires: Option<i64>,
}

/// What the UI shows on the MyFitnessPal row.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct MfpStatus {
    connected: bool,
    saved_at: Option<String>,
    session_expires: Option<i64>,
}

impl MfpStatus {
    const DISCONNECTED: Self = Self {
        connected: false,
        saved_at: None,
        session_expires: None,
    };
}

fn session_path(app: &AppHandle) -> Result<PathBuf, String> {
    let dir = app
        .path()
        .app_data_dir()
        .map_err(|e| format!("cannot find the app data folder: {e}"))?;
    fs::create_dir_all(&dir).map_err(|e| format!("cannot create {}: {e}", dir.display()))?;
    Ok(dir.join(SESSION_FILE))
}

fn is_mfp_domain(domain: &str) -> bool {
    let domain = domain.trim_start_matches('.');
    domain == "myfitnesspal.com" || domain.ends_with(".myfitnesspal.com")
}

/// Keeps the cookies of myfitnesspal.com and its subdomains. Filtering by suffix matters:
/// `cookies_for_url` drops cookies set on ".myfitnesspal.com" (`cf_clearance`).
fn mfp_cookies(all: Vec<Cookie<'static>>) -> Vec<SavedCookie> {
    all.into_iter()
        .filter_map(|c| {
            let domain = c.domain()?;
            is_mfp_domain(domain).then(|| SavedCookie {
                name: c.name().to_owned(),
                value: c.value().to_owned(),
                domain: domain.to_owned(),
                path: c.path().unwrap_or("/").to_owned(),
                expires: c
                    .expires_datetime()
                    .map_or(0, OffsetDateTime::unix_timestamp),
            })
        })
        .collect()
}

/// Builds the session from the window's cookies, or `None` before MyFitnessPal has set
/// the session cookie.
fn session_from(cookies: Vec<SavedCookie>, saved_at: String) -> Option<SessionFile> {
    let expires = cookies.iter().find(|c| c.name == SESSION_COOKIE)?.expires;
    Some(SessionFile {
        cookies,
        user_agent: USER_AGENT.to_owned(),
        saved_at: Some(saved_at),
        session_expires: Some(expires),
    })
}

/// Writes `data` to `path` with mode 0600 through a temporary file and a rename, so a
/// reader never sees half a file and a crash never leaves the old file truncated.
fn write_private(path: &Path, data: &[u8]) -> io::Result<()> {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_nanos());
    let tmp = path.with_file_name(format!(
        ".{SESSION_FILE}.{}.{nanos}.tmp",
        std::process::id()
    ));
    let written = (|| {
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&tmp)?;
        file.write_all(data)?;
        file.sync_all()?;
        fs::rename(&tmp, path)
    })();
    if written.is_err() {
        // Best effort: the error that matters is the one returned below.
        let _ = fs::remove_file(&tmp);
    }
    written
}

/// Removes the MyFitnessPal cookies from the main webview's store, which the login window
/// shares. Best effort: a cookie left behind only means the next sign in page is prefilled.
fn clear_mfp_cookies(app: &AppHandle) {
    let Some(main) = app.get_webview_window("main") else {
        return;
    };
    let Ok(all) = main.cookies() else { return };
    for cookie in all
        .into_iter()
        .filter(|c| c.domain().is_some_and(is_mfp_domain))
    {
        let _ = main.delete_cookie(cookie);
    }
}

/// A file that can't be read or doesn't hold a JSON object means not connected.
fn status_from_file(path: &Path) -> MfpStatus {
    let Ok(bytes) = fs::read(path) else {
        return MfpStatus::DISCONNECTED;
    };
    // serde reads a struct from a JSON array too, so check for an object first.
    let Ok(object @ serde_json::Value::Object(_)) = serde_json::from_slice(&bytes) else {
        return MfpStatus::DISCONNECTED;
    };
    match serde_json::from_value::<SessionFile>(object) {
        Ok(s) => MfpStatus {
            connected: true,
            saved_at: s.saved_at,
            session_expires: s.session_expires,
        },
        Err(_) => MfpStatus::DISCONNECTED,
    }
}

/// A missing file is already disconnected; any other error (permissions) must reach the UI.
fn remove_session(path: &Path) -> Result<(), String> {
    match fs::remove_file(path) {
        Err(e) if e.kind() != io::ErrorKind::NotFound => {
            Err(format!("cannot delete {}: {e}", path.display()))
        }
        _ => Ok(()),
    }
}

/// Saves the session once the user is past the login page. The caller runs it on a
/// task, not in the page load callback, because `cookies()` pumps the main run loop.
fn save_session(win: &WebviewWindow, path: &Path, saved: &AtomicBool) {
    let Ok(all) = win.cookies() else { return };
    let saved_at = OffsetDateTime::now_utc()
        .format(&Rfc3339)
        .unwrap_or_default();
    let Some(session) = session_from(mfp_cookies(all), saved_at) else {
        return;
    };
    if saved.swap(true, Ordering::SeqCst) {
        return;
    }
    let written = serde_json::to_vec(&session)
        .map_err(io::Error::other)
        .and_then(|body| write_private(path, &body));
    if written.is_err() {
        // Let the next page load try again.
        saved.store(false, Ordering::SeqCst);
        return;
    }
    let _ = win
        .app_handle()
        .emit("mfp-connected", status_from_file(path));
    let _ = win.close();
}

#[tauri::command]
#[expect(
    clippy::needless_pass_by_value,
    reason = "Tauri passes command arguments by value"
)]
pub fn mfp_status(app: AppHandle) -> Result<MfpStatus, String> {
    Ok(status_from_file(&session_path(&app)?))
}

#[tauri::command]
pub async fn mfp_connect(app: AppHandle) -> Result<(), String> {
    if let Some(win) = app.get_webview_window(LOGIN_WINDOW) {
        let _ = win.set_focus();
        return Ok(());
    }
    let path = session_path(&app)?;
    // Start fresh so a stale token is never saved again. Closing the window without
    // signing in therefore leaves MyFitnessPal disconnected.
    remove_session(&path)?;
    clear_mfp_cookies(&app);
    let url = LOGIN_URL.parse().expect("LOGIN_URL is a valid URL");
    let saved = Arc::new(AtomicBool::new(false));
    WebviewWindowBuilder::new(&app, LOGIN_WINDOW, WebviewUrl::External(url))
        .title("Connect MyFitnessPal")
        .inner_size(480.0, 720.0)
        .user_agent(USER_AGENT)
        .on_page_load(move |win, payload| {
            let url = payload.url();
            let past_login = payload.event() == PageLoadEvent::Finished
                && url.scheme() == "https"
                && url.host_str().is_some_and(is_mfp_domain)
                && !url.path().starts_with(LOGIN_PATH);
            if past_login {
                let (win, path, saved) = (win.clone(), path.clone(), Arc::clone(&saved));
                tauri::async_runtime::spawn(async move { save_session(&win, &path, &saved) });
            }
        })
        .build()
        .map_err(|e| format!("cannot open the MyFitnessPal window: {e}"))?;
    Ok(())
}

#[tauri::command]
pub async fn mfp_disconnect(app: AppHandle) -> Result<(), String> {
    // destroy, not close: a close request lets the window live on, and a page load in it
    // could save a new session after the file is gone.
    if let Some(win) = app.get_webview_window(LOGIN_WINDOW) {
        let _ = win.destroy();
    }
    let path = session_path(&app)?;
    clear_mfp_cookies(&app);
    remove_session(&path)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    fn cookie(name: &'static str, domain: &'static str) -> Cookie<'static> {
        Cookie::build((name, "v"))
            .domain(domain)
            .path("/")
            .build()
            .into_owned()
    }

    fn saved(name: &str, expires: i64) -> SavedCookie {
        SavedCookie {
            name: name.into(),
            value: "v".into(),
            domain: "www.myfitnesspal.com".into(),
            path: "/".into(),
            expires,
        }
    }

    /// A fresh folder per test, removed when the guard drops.
    struct TempDir(PathBuf);
    impl TempDir {
        fn new(name: &str) -> Self {
            let dir = std::env::temp_dir().join(format!("mfp-{name}-{}", std::process::id()));
            let _ = fs::remove_dir_all(&dir);
            fs::create_dir_all(&dir).expect("create temp dir");
            Self(dir)
        }
    }
    impl Drop for TempDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    #[test]
    fn keeps_only_myfitnesspal_cookies_including_dot_domains() {
        let kept = mfp_cookies(vec![
            cookie(SESSION_COOKIE, "www.myfitnesspal.com"),
            cookie("cf_clearance", ".myfitnesspal.com"),
            cookie("other", "evilmyfitnesspal.com"),
            cookie("ga", "google.com"),
            cookie("root", "myfitnesspal.com"),
            cookie("evil2", "myfitnesspal.com.evil.com"),
            Cookie::build(("nodomain", "v")).build().into_owned(),
        ]);
        let names: Vec<&str> = kept.iter().map(|c| c.name.as_str()).collect();
        assert_eq!(names, [SESSION_COOKIE, "cf_clearance", "root"]);
    }

    #[test]
    fn is_mfp_domain_cases() {
        for ok in [
            "myfitnesspal.com",
            ".myfitnesspal.com",
            "www.myfitnesspal.com",
        ] {
            assert!(is_mfp_domain(ok), "{ok}");
        }
        for bad in [
            "",
            "myfitnesspal.com.evil.com",
            "evilmyfitnesspal.com",
            "google.com",
        ] {
            assert!(!is_mfp_domain(bad), "{bad}");
        }
    }

    #[test]
    fn no_session_before_the_session_cookie_exists() {
        assert!(session_from(vec![saved("cf_clearance", 9)], "t".into()).is_none());
    }

    #[test]
    fn session_file_has_the_fields_the_go_core_reads() {
        let session = session_from(
            vec![saved(SESSION_COOKIE, 1_900_000_000)],
            "2026-10-25T10:00:00Z".into(),
        )
        .expect("session cookie present");
        let json: serde_json::Value = serde_json::to_value(&session).expect("serialize");
        assert_eq!(
            json,
            serde_json::json!({
                "cookies": [{"name": SESSION_COOKIE, "value": "v", "domain": "www.myfitnesspal.com", "path": "/", "expires": 1_900_000_000}],
                "user_agent": USER_AGENT,
                "saved_at": "2026-10-25T10:00:00Z",
                "session_expires": 1_900_000_000,
            })
        );
    }

    #[test]
    fn write_private_replaces_the_file_with_mode_0600_and_leaves_no_temp_file() {
        let dir = TempDir::new("write");
        let path = dir.0.join(SESSION_FILE);
        fs::write(&path, "old").expect("seed");

        write_private(&path, b"new").expect("write");

        assert_eq!(fs::read_to_string(&path).expect("read"), "new");
        assert_eq!(
            fs::metadata(&path).expect("stat").permissions().mode() & 0o777,
            0o600
        );
        assert_eq!(
            fs::read_dir(&dir.0).expect("list").count(),
            1,
            "a temp file was left behind"
        );
    }

    #[test]
    fn status_reads_saved_at_and_expiry_from_the_file() {
        let dir = TempDir::new("status");
        let path = dir.0.join(SESSION_FILE);
        fs::write(
            &path,
            r#"{"cookies":[],"saved_at":"2026-10-25T10:00:00Z","session_expires":42}"#,
        )
        .expect("seed");

        let status = status_from_file(&path);

        assert_eq!(
            status,
            MfpStatus {
                connected: true,
                saved_at: Some("2026-10-25T10:00:00Z".into()),
                session_expires: Some(42)
            }
        );
    }

    #[test]
    fn status_is_disconnected_for_a_missing_or_broken_file() {
        let dir = TempDir::new("broken");
        let path = dir.0.join(SESSION_FILE);
        assert_eq!(status_from_file(&path), MfpStatus::DISCONNECTED);
        for broken in ["not json", "[]", r#"{"saved_at": 5}"#] {
            fs::write(&path, broken).expect("seed");
            assert_eq!(status_from_file(&path), MfpStatus::DISCONNECTED, "{broken}");
        }
    }

    #[test]
    fn remove_session_ignores_a_missing_file_and_reports_other_errors() {
        let dir = TempDir::new("remove");
        let path = dir.0.join(SESSION_FILE);
        fs::create_dir_all(&path).expect("seed"); // a folder where the file should be
        assert!(remove_session(&path).is_err());
        fs::remove_dir(&path).expect("cleanup");
        assert_eq!(remove_session(&path), Ok(()));
    }
}
