import { GarminConnection } from './garmin-connection.js';
import { HeroPanel } from './hero-panel.js';
import { LlmPackSheet } from './llm-pack-sheet.js';
import { MfpConnection } from './mfp-connection.js';
import { RangePicker } from './range-picker.js';
import { RecoveryCard } from './recovery-card.js';
import { RunSteps } from './run-steps.js';

/** A 90-day mock export with MyFitnessPal runs ~19 s of real timers. */
const LONGEST_RUN_MS = 60_000;

/**
 * The single window of the app, composed from one page object per component.
 * Collaborators come in through the constructor: `scenario` is anything with
 * `install(page)` and `clock` anything with `install()`.
 */
export class ExportApp {
  #page;
  #scenario;
  #clock;

  constructor(page, { scenario, clock }) {
    this.#page = page;
    this.#scenario = scenario;
    this.#clock = clock;
    this.range = new RangePicker(page);
    this.garmin = new GarminConnection(page);
    this.mfp = new MfpConnection(page);
    this.hero = new HeroPanel(page);
    this.run = new RunSteps(page);
    this.recovery = new RecoveryCard(page);
    this.sheet = new LlmPackSheet(page);
    this.toast = page.locator('#toast');
  }

  async open() {
    await this.#scenario.install(this.#page);
    await this.#clock.install();
    await this.#page.goto('/');
    await this.#page.waitForFunction(() => !/…/.test(document.getElementById('ver').textContent)); // boot() finished
  }

  /** Quits and starts the app again: localStorage (the app's memory) survives, as in the real app. */
  async restart() {
    await this.#page.reload();
    await this.#page.waitForFunction(() => !/…/.test(document.getElementById('ver').textContent));
  }

  /** Pull: runs from the saved Garmin login, no credentials. */
  async startExport() {
    await this.hero.pressCta();
  }

  /** ⌘↵ on macOS, Ctrl↵ elsewhere, as the CTA's <kbd> shows. */
  async pressStartShortcut() {
    await this.#page.keyboard.press('ControlOrMeta+Enter');
  }

  async pressEscape() {
    await this.#page.keyboard.press('Escape');
  }

  /** Waits until the hero leaves "running" (done, error or cancelled). */
  async waitForRunEnd() {
    await this.#page.waitForFunction(() => document.getElementById('hero').dataset.state !== 'running', null, { timeout: LONGEST_RUN_MS });
  }

  /** Waits until the Run row of `phase` shows `status`: the running spinner by default. */
  async waitForPhase(phase, status = 'run') {
    await this.#page.waitForFunction(([p, st]) => document.querySelector(`#rows li[data-phase="${p}"]`).dataset.st === st, [phase, status], { timeout: LONGEST_RUN_MS });
  }
}
