// Stage 102.1.1: the store app's service worker. It lets the installed app
// open without a connection: app files are fetched from the network first - so
// a deploy shows on the next open - and the cached copy is used only when the
// network fails. API calls are never touched: data always comes live from the
// server (decision D3). Scope: /store/ only.
//
// Stage 102.1.3: it also shows push alerts (approvals, low stock) when the app
// is closed, and opens the right screen when one is tapped. Stage 102.3.3: the
// count screen's module is kept too, so a count can be done offline.
const CACHE = 'store-app-v2';
const SHELL = ['./', 'store.css?v=2', 'store.js?v=2', 'store-stock.js?v=2', 'manifest.json', 'icon.svg', 'icon-192.png', 'apple-touch-icon.png'];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys.filter(k => k.startsWith('store-app-') && k !== CACHE).map(k => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin || !url.pathname.startsWith('/store/')) return;
  event.respondWith(
    fetch(req)
      .then((res) => {
        if (res.ok && res.type === 'basic' && !res.redirected) {
          const copy = res.clone();
          caches.open(CACHE).then(cache => cache.put(req, copy));
        }
        return res;
      })
      .catch(() => caches.match(req).then(hit => hit || (req.mode === 'navigate' ? caches.match('./') : undefined)))
      .then(res => res || new Response('Offline', { status: 503, statusText: 'Offline' })),
  );
});

// A push is { title, body, url, tag }, encrypted by the server for this phone
// alone (RFC 8291). A newer alert of the same tag replaces the older one.
self.addEventListener('push', (event) => {
  let msg = {};
  try { msg = event.data ? event.data.json() : {}; } catch (_) { msg = { body: event.data ? event.data.text() : '' }; }
  event.waitUntil(self.registration.showNotification(msg.title || 'Store', {
    body: msg.body || '',
    tag: msg.tag || undefined,
    renotify: !!msg.tag,
    icon: 'icon-192.png',
    badge: 'icon-192.png',
    data: { url: msg.url || '/store/#/home' },
  }));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL((event.notification.data && event.notification.data.url) || '/store/#/home', self.location.origin);
  if (target.origin !== self.location.origin || !target.pathname.startsWith('/store/')) return;
  event.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
    const open = list.find(c => new URL(c.url).pathname.startsWith('/store/'));
    if (open) return open.focus().then(c => c.navigate ? c.navigate(target.href) : c);
    return self.clients.openWindow(target.href);
  }));
});
