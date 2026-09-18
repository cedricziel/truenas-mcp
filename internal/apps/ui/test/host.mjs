// A fake MCP Apps host, driven from Playwright.
//
// The real host is a chat client; this stands in for exactly the part an app
// sees: a parent window that loads the view into a sandboxed iframe and speaks
// JSON-RPC 2.0 over postMessage. It answers ui/initialize, records every
// message the view sends, and lets a test push tool results in and answer the
// view's own tools/call requests.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));

export function appHTML(name) {
  return readFileSync(path.join(here, '..', `${name}.html`), 'utf8');
}

// The CSP a host applies to a view that declares none, per the extension.
// It is enforced here through a <meta> tag inside the iframe document, which
// is what a srcdoc host can do, so an app that reaches off-document fails in
// the test the way it would in production.
export const defaultCSP = [
  "default-src 'none'",
  "script-src 'self' 'unsafe-inline'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "media-src 'self' data:",
  "object-src 'none'",
  "connect-src 'none'",
].join('; ');

export function hostPage() {
  return `<!doctype html><html><body>
<script>
  window.__sent = [];      // every message the view sent, in order
  window.__toolCalls = []; // tools/call requests awaiting an answer: {id, params}
  window.__hostContext = { theme: 'light' };
  window.addEventListener('message', (ev) => {
    const m = ev.data;
    if (!m || m.jsonrpc !== '2.0') return;
    window.__sent.push(m);
    if (m.method === 'ui/initialize') {
      window.__reply(m.id, { protocolVersion: '2025-06-18', hostCapabilities: {}, hostContext: window.__hostContext });
    } else if (m.method === 'tools/call') {
      window.__toolCalls.push({ id: m.id, params: m.params });
    }
  });
  window.__view = () => document.getElementById('view').contentWindow;
  window.__reply = (id, result) => window.__view().postMessage({ jsonrpc: '2.0', id, result }, '*');
  window.__fail = (id, message) => window.__view().postMessage({ jsonrpc: '2.0', id, error: { code: -32000, message } }, '*');
  window.__notify = (method, params) => window.__view().postMessage({ jsonrpc: '2.0', method, params }, '*');
  window.__request = (id, method, params) => window.__view().postMessage({ jsonrpc: '2.0', id, method, params }, '*');
</script>
<iframe id="view" sandbox="allow-scripts allow-same-origin" style="width:900px;height:700px;border:0"></iframe>
</body></html>`;
}

// mount loads a view into the host page and waits for it to finish the
// initialize handshake.
export async function mount(page, name, { csp = defaultCSP, hostContext = { theme: 'light' } } = {}) {
  await page.setContent(hostPage());
  await page.evaluate((ctx) => { window.__hostContext = ctx; }, hostContext);
  const html = appHTML(name).replace(
    '<head>',
    `<head><meta http-equiv="Content-Security-Policy" content="${csp}">`,
  );
  await page.locator('#view').evaluate((el, doc) => { el.srcdoc = doc; }, html);
  await page.waitForFunction(() =>
    window.__sent.some((m) => m.method === 'ui/notifications/initialized'));
  return page.frameLocator('#view');
}

export const sent = (page) => page.evaluate(() => window.__sent);
export const notify = (page, method, params) => page.evaluate(([m, p]) => window.__notify(m, p), [method, params]);
export const toolCalls = (page) => page.evaluate(() => window.__toolCalls);
export const reply = (page, id, result) => page.evaluate(([i, r]) => window.__reply(i, r), [id, result]);
export const fail = (page, id, message) => page.evaluate(([i, m]) => window.__fail(i, m), [id, message]);
export const request = (page, id, method, params) =>
  page.evaluate(([i, m, p]) => window.__request(i, m, p), [id, method, params]);
