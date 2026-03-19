// Service worker for PWA installability.
// Pass-through fetch (no offline caching — everything comes from the local daemon).
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', () => self.clients.claim());
self.addEventListener('fetch', e => e.respondWith(fetch(e.request)));
