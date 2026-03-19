// Minimal service worker — required for PWA installability.
// No offline caching; everything fetches from the local daemon.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', () => self.clients.claim());
