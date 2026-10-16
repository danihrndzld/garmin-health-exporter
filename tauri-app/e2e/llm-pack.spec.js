// LLM pack sheet after an export. State Transition (closed ↔ open), Equivalence
// Partitioning over language (es | en) and build mode (folder | one file), and
// Error Guessing on a write failure (EACCES), which the mock makes reproducible.
// Oracle: CLAUDE.md "LLM pack" (one language per subfolder llm-<lang>/ or
// llm-<lang>-one-file/) and the ES/EN strings the sheet ships with.
import { test, expect } from './fixtures.js';

async function finishedExport(app) {
  await app.open();
  await app.startExport();
  await app.waitForRunEnd();
}

test.describe('with a finished export', () => {
  test.beforeEach(async ({ app }) => { await finishedExport(app); });

  test('ST-T13 Done + L opens the sheet, focus on the main build button', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await expect(app.sheet.dialog).toBeVisible();
    await expect(app.sheet.folderButton).toBeFocused();
  });

  test('ST-T14 open sheet + Esc closes it, focus back on LLM pack', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await app.sheet.closeWithEscape();
    await expect(app.sheet.dialog).toBeHidden();
    await expect(app.hero.llmButton).toBeFocused();
  });

  test('ST-I8 typing in a field + L: sheet stays closed', async ({ app }) => {
    await app.range.choose('Custom');
    await app.range.from.focus();
    await app.sheet.openWithShortcut();
    await expect(app.sheet.dialog).toBeHidden();
  });

  test('EP-08 language es: Spanish labels, target llm-es/', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await app.sheet.language('es');
    await expect(app.sheet.title).toHaveText('Pásaselo a un LLM.');
    await expect(app.sheet.folderLabel).toHaveText('Armar carpeta: contexto + CSV');
    await expect(app.sheet.folderTarget).toHaveText('llm-es/');
  });

  test('EP-09 language en: English labels, target llm-en/', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await app.sheet.language('en');
    await expect(app.sheet.title).toHaveText('Hand it to an LLM.');
    await expect(app.sheet.folderLabel).toHaveText('Build folder: context + CSVs');
    await expect(app.sheet.folderTarget).toHaveText('llm-en/');
  });

  test('EP-10 build folder: context + 10 CSVs = 11 files', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await app.sheet.buildFolder();
    await expect(app.sheet.folderLabel).toHaveText('Listo · 11 archivos');
    await expect(app.toast).toHaveText('Arrastra todo lo de la carpeta al chat.');
  });

  test('EP-11 build one file: 1 file, singular label', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await app.sheet.buildOneFile();
    await expect(app.sheet.oneFileLabel).toHaveText('Listo · 1 archivo');
    await expect(app.toast).toHaveText('Arrastra el archivo al chat.');
  });
});

test('EG-03 pack write fails (EACCES): error toast, button label restored', async ({ app, scenario }) => {
  scenario.packFails();
  await finishedExport(app);
  await app.sheet.openWithShortcut();
  await app.sheet.buildFolder();
  await expect(app.toast).toHaveText('No se pudo armar: EACCES: permission denied');
  await expect(app.sheet.folderButton).toHaveAttribute('data-state', 'error');
  await expect(app.sheet.folderLabel).toHaveText('Armar carpeta: contexto + CSV');
});

test('ST-T18 Garmin off run → restart → the sheet still offers the pack', async ({ app, scenario }) => {
  scenario.mfpConnected();
  await app.open();
  await app.garmin.include.uncheck();
  await app.startExport();
  await app.waitForRunEnd();
  await app.restart();
  await app.sheet.openWithShortcut();
  await expect(app.sheet.dialog).toBeVisible();
  await expect(app.sheet.emptyNote).toBeHidden();
  await expect(app.sheet.folderButton).toBeFocused();
});
