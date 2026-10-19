// Stage 102.1.1: the store app's service worker. Its only job is letting the
// installed app open without a connection. App files are fetched from the
// network first - so a deploy shows on the next open - and the cached copy is
// used only when the network fails. API calls are never touched: data always
// comes live from the server (decision D3, 2026-10-19). Scope: /store/ only.
const CACHE = 'store-app-v1';
const SHELL = ['./', 'store.css?v=1', 'store.js?v=1', 'manifest.json', 'icon.svg', 'icon-192.png', 'apple-touch-icon.png'];

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
