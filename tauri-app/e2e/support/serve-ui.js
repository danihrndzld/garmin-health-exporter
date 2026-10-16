// Static server for ui/ during e2e runs. Python's http.server listens with a
// backlog of 5 and drops page loads when every Playwright worker starts at once.
const root = new URL('../../ui/', import.meta.url).pathname;

Bun.serve({
  hostname: '127.0.0.1',
  port: Number(process.env.PORT),
  async fetch(req) {
    const path = new URL(req.url).pathname; // normalized: no ".." segments survive
    const file = Bun.file(root + (path === '/' ? 'index.html' : path.slice(1)));
    return (await file.exists()) ? new Response(file) : new Response('Not found', { status: 404 });
  },
});
