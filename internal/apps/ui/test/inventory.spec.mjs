import { test, expect } from '@playwright/test';
import { mount, sent, notify, toolCalls, reply, fail, request } from './host.mjs';

// The same shape internal/server/inventory.go produces; see InventoryOutput.
const inventory = {
  host: { hostname: 'hive', version: 'TrueNAS-26.04.0', uptime: '3 days, 2:10:05' },
  pools: [
    { name: 'tank', status: 'ONLINE', healthy: true, size: 4_000_000_000_000, allocated: 3_600_000_000_000, free: 400_000_000_000 },
    { name: 'scratch', status: 'DEGRADED', healthy: false, size: 1000, allocated: 100, free: 900 },
  ],
  datasets: [
    { name: 'tank', pool: 'tank', type: 'FILESYSTEM', used: 4096, available: 2048 },
    { name: 'tank/media', pool: 'tank', type: 'FILESYSTEM', used: 1024, available: 2048 },
  ],
  apps: [{ name: 'paperless', state: 'RUNNING', version: '2.1.0', upgrade_available: true, image_updates_available: false }],
  vms: [{ name: 'win11', state: 'STOPPED', vcpus: 4, memory_mib: 8192 }],
  containers: [],
  shares: [
    { kind: 'smb', name: 'media', path: '/mnt/tank/media', enabled: true },
    { kind: 'nfs', name: 'backups', path: '/mnt/tank/backups', enabled: false },
  ],
  alerts: [{ level: 'WARNING', message: 'Pool scratch is DEGRADED', since: '2025-09-16T00:00:00Z' }],
  generated_at: '2026-09-18T12:00:00Z',
};

const toolResult = (structuredContent, extra = {}) => ({
  content: [{ type: 'text', text: JSON.stringify(structuredContent) }],
  structuredContent,
  ...extra,
});

test('performs the initialize handshake before anything else', async ({ page }) => {
  await mount(page, 'inventory');
  const messages = await sent(page);
  expect(messages[0].method).toBe('ui/initialize');
  expect(messages[0].params.protocolVersion).toBe('2026-01-26');
  expect(messages[0].params.appInfo.name).toBe('truenas-inventory');
  expect(messages[1].method).toBe('ui/notifications/initialized');
});

test('renders a tool result into tiles and sections', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult(inventory));

  await expect(view.locator('#title')).toHaveText('hive inventory');
  await expect(view.locator('#subtitle')).toContainText('TrueNAS-26.04.0');
  await expect(view.locator('[data-tile="pools"] .n')).toHaveText('2');
  await expect(view.locator('[data-tile="apps"] .n')).toHaveText('1');
  await expect(view.locator('[data-tile="containers"] .n')).toHaveText('0');

  const pools = view.locator('[data-section="pools"]');
  await expect(pools.locator('tbody tr')).toHaveCount(2);
  await expect(pools.locator('tbody tr').nth(0)).toContainText('3.3 TiB of 3.6 TiB (90%)');
  await expect(pools.locator('tbody tr').nth(0).locator('.bar')).toHaveClass(/bad/);
  await expect(pools.locator('tbody tr').nth(1).locator('td').nth(1)).toHaveClass(/warn/);

  await expect(view.locator('[data-section="apps"] tbody tr')).toContainText('app upgrade');
  await expect(view.locator('[data-section="shares"] tbody tr').nth(1)).toContainText('no');
  await expect(view.locator('[data-section="alerts"] tbody tr')).toContainText('Pool scratch is DEGRADED');
  await expect(view.locator('[data-section="containers"] .empty')).toHaveText('None.');
  await expect(view.locator('#status')).toHaveText('As of 2026-09-18T12:00:00Z');
});

test('reports its size to the host after rendering', async ({ page }) => {
  await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult(inventory));
  await page.waitForFunction(() => window.__sent.some((m) => m.method === 'ui/notifications/size-changed'));
  const size = (await sent(page)).find((m) => m.method === 'ui/notifications/size-changed');
  expect(size.params.height).toBeGreaterThan(100);
  expect(size.params.width).toBeGreaterThan(100);
});

test('falls back to the text content when no structured content arrives', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', { content: [{ type: 'text', text: JSON.stringify(inventory) }] });
  await expect(view.locator('[data-tile="pools"] .n')).toHaveText('2');
});

test('shows a refused section as unavailable without hiding the rest', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult({
    ...inventory,
    apps: [],
    errors: { apps: 'this API key is not permitted to read it' },
  }));
  await expect(view.locator('[data-tile="apps"] .n')).toHaveText('—');
  await expect(view.locator('[data-section="apps"] .err')).toContainText('not permitted');
  await expect(view.locator('[data-section="pools"] tbody tr')).toHaveCount(2);
});

test('shows an error result as a failure', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', { isError: true, content: [{ type: 'text', text: 'session expired' }] });
  await expect(view.locator('#status')).toHaveText('Inventory failed: session expired');
  await expect(view.locator('#view')).toBeHidden();
});

test('filters every section by name', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult(inventory));
  await view.locator('#filter').fill('media');

  await expect(view.locator('[data-section="pools"] .empty')).toHaveText('No match.');
  await expect(view.locator('[data-section="datasets"] tbody tr')).toHaveCount(1);
  await expect(view.locator('[data-section="datasets"] h2')).toHaveText('Datasets (1 of 2)');
  await expect(view.locator('[data-section="shares"] tbody tr')).toHaveCount(1);

  await view.locator('#filter').fill('');
  await expect(view.locator('[data-section="pools"] tbody tr')).toHaveCount(2);
});

test('refresh calls the inventory tool through the host and renders the answer', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult(inventory));
  await view.locator('#refresh').click();

  await expect(view.locator('#refresh')).toBeDisabled();
  await page.waitForFunction(() => window.__toolCalls.length === 1);
  const [call] = await toolCalls(page);
  expect(call.params).toEqual({ name: 'inventory', arguments: {} });

  await reply(page, call.id, toolResult({ ...inventory, pools: inventory.pools.slice(0, 1) }));
  await expect(view.locator('[data-tile="pools"] .n')).toHaveText('1');
  await expect(view.locator('#refresh')).toBeEnabled();
});

test('a refused refresh leaves the last result on screen', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult(inventory));
  await view.locator('#refresh').click();
  await page.waitForFunction(() => window.__toolCalls.length === 1);
  const [call] = await toolCalls(page);
  await fail(page, call.id, 'user declined');

  await expect(view.locator('#status')).toHaveText('Refresh failed: user declined');
  await expect(view.locator('[data-tile="pools"] .n')).toHaveText('2');
  await expect(view.locator('#refresh')).toBeEnabled();
});

test('tool-input shows progress and tool-cancelled explains why', async ({ page }) => {
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-input', { arguments: {} });
  await expect(view.locator('#status')).toHaveText('Collecting inventory…');
  await expect(view.locator('#refresh')).toBeDisabled();
  await notify(page, 'ui/notifications/tool-cancelled', { reason: 'user interrupted' });
  await expect(view.locator('#status')).toHaveText('Cancelled: user interrupted');
  await expect(view.locator('#refresh')).toBeEnabled();
});

test('follows the host theme at initialize and when it changes', async ({ page }) => {
  const view = await mount(page, 'inventory', { hostContext: { theme: 'dark' } });
  await expect(view.locator('html')).toHaveAttribute('data-theme', 'dark');
  await notify(page, 'ui/notifications/host-context-changed', { theme: 'light' });
  await expect(view.locator('html')).toHaveAttribute('data-theme', 'light');
});

test('answers a teardown request so the host can unload it', async ({ page }) => {
  await mount(page, 'inventory');
  await request(page, 99, 'ui/resource-teardown', {});
  await page.waitForFunction(() => window.__sent.some((m) => m.id === 99 && 'result' in m));
});

test('runs under the default CSP without a console error', async ({ page }) => {
  const errors = [];
  page.on('console', (msg) => { if (msg.type() === 'error') errors.push(msg.text()); });
  page.on('pageerror', (err) => errors.push(String(err)));
  const view = await mount(page, 'inventory');
  await notify(page, 'ui/notifications/tool-result', toolResult(inventory));
  await expect(view.locator('[data-tile="pools"] .n')).toHaveText('2');
  expect(errors).toEqual([]);
});
