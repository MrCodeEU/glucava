// glucava service worker. Served at /sw.js (root scope) with the two
// placeholders below filled in by the server (see pwa.go).
//
// It does two things only:
//  1. shows push notifications and opens the app when one is tapped;
//  2. keeps the static app shell (CSS, JS, icons) for fast, offline-tolerant
//     starts.
// Pages, API calls and glucose data are never cached: they always come from
// the network, so nothing sensitive is stored on the device.
const BUILD = __BUILD__;
const ASSETS = __ASSETS__;
const CACHE = 'glucava-shell-' + BUILD;
const DEV = BUILD === 'dev'; // a dev build changes files in place: never cache

self.addEventListener('install', (event) => {
  event.waitUntil(
    (DEV ? Promise.resolve() : caches.open(CACHE).then((c) => c.addAll(ASSETS))).then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith('glucava-shell-') && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

const OFFLINE =
  '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">' +
  '<title>glucava</title><body style="font:16px system-ui;padding:2rem;max-width:32rem;margin:auto">' +
  '<h1>You are offline</h1><p>glucava needs a connection to show your data. Try again when you are back online.</p>';

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (DEV || req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  if (url.pathname.startsWith('/static/')) {
    // Precached files answer from the cache; any other static file goes to the network.
    event.respondWith(caches.match(req).then((hit) => hit || fetch(req)));
    return;
  }
  if (req.mode === 'navigate') {
    event.respondWith(fetch(req).catch(() => new Response(OFFLINE, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } })));
  }
  // Everything else (actions, streams, exports) is left to the browser.
});

self.addEventListener('push', (event) => {
  let d = {};
  try {
    d = event.data ? event.data.json() : {};
  } catch (e) {
    d = { body: event.data ? event.data.text() : '' };
  }
  const title = String(d.title || 'glucava');
  event.waitUntil(
    self.registration.showNotification(title, {
      body: String(d.body || ''),
      tag: d.tag ? String(d.tag) : undefined,
      icon: '/static/icon-192.png',
      badge: '/static/badge-96.png',
      data: { url: typeof d.url === 'string' ? d.url : '/' },
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  // Only ever open a page of this app, whatever the payload says.
  let target = new URL('/', self.location.origin);
  try {
    const u = new URL(event.notification.data && event.notification.data.url, self.location.origin);
    if (u.origin === self.location.origin) target = u;
  } catch (e) {}
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((wins) => {
      for (const w of wins) {
        if (new URL(w.url).origin === target.origin && 'focus' in w) {
          return w.focus().then((c) => ('navigate' in c ? c.navigate(target.href) : c));
        }
      }
      return self.clients.openWindow(target.href);
    }),
  );
});
