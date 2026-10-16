import { test as base, expect } from '@playwright/test';
import { ExportApp } from './pages/export-app.js';
import { FixedToday } from './support/fixed-today.js';
import { LayoutProbe } from './support/layout-probe.js';
import { MockBackend } from './support/mock-backend.js';

/**
 * Composition root: the only place that wires concrete collaborators. Specs ask
 * for `scenario` (configure before `app.open()`), `app` and, for layout, `layout`.
 */
export const test = base.extend({
  scenario: async ({}, use) => { await use(new MockBackend()); },
  clock: async ({ page }, use) => { await use(new FixedToday(page)); },
  app: async ({ page, scenario, clock }, use) => { await use(new ExportApp(page, { scenario, clock })); },
  layout: async ({ page }, use) => { await use(new LayoutProbe(page)); },
});

export { expect };
