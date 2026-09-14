// Clawdline Cloud's service worker. The build marker is filled by build-web-app.py. It makes the
// worker bytes change with the immutable app bundle, so an installed PWA has a path off a document
// WebKit kept alive from an older launch.
var CLAWDLINE_BUILD = "__CLAWDLINE_BUILD__";
var UPDATE_BRIDGE_CACHE = "clawdline-pwa-update-bridge-v1";
var UPDATE_BRIDGE_URL = "/.clawdline-pwa-update-bridge-v1";

self.addEventListener("install", function () { self.skipWaiting(); });

self.addEventListener("activate", function (event) {
    event.waitUntil(
        // Builds before the update bridge could install and claim a new worker while the old page
        // kept executing indefinitely. Navigate each existing client once, then retain a durable
        // origin-local marker so later app releases do not interrupt somebody who is typing.
        caches.open(UPDATE_BRIDGE_CACHE).then(function (cache) {
            return cache.match(UPDATE_BRIDGE_URL).then(function (recorded) {
                if (recorded) return false;
                return cache.put(UPDATE_BRIDGE_URL, new Response(CLAWDLINE_BUILD)).then(function () {
                    return true;
                });
            });
        }).catch(function () { return true; }).then(function (bridge) {
            return self.clients.claim().then(function () {
                if (!bridge) return null;
                return self.clients.matchAll({ type: "window", includeUncontrolled: true })
                    .then(function (windows) {
                        return Promise.all(windows.map(function (client) {
                            if (!client || typeof client.navigate !== "function" || !client.url) return null;
                            return client.navigate(client.url).catch(function () { return null; });
                        }));
                    });
            });
        })
    );
});

// Never cache the app document in the worker. The HTML is the small mutable pointer to an
// immutable asset tree and must be revalidated whenever the PWA genuinely navigates.
self.addEventListener("fetch", function (event) {
    if (event.request.mode !== "navigate") return;
    event.respondWith(
        fetch(event.request.url, { cache: "reload", credentials: "include" })
            .catch(function () { return fetch(event.request); })
    );
});

self.addEventListener("push", function (event) {
    var payload = {};
    try { payload = event.data ? event.data.json() : {}; } catch (e) { }
    // Apple's declarative payload is also the legacy fallback delivered to this handler on
    // browsers that do not understand `web_push: 8030` themselves.
    var notification = payload.notification && typeof payload.notification === "object"
        ? payload.notification : payload;
    var destination = notification.navigate || notification.url || payload.url || "/";
    event.waitUntil(self.registration.showNotification(notification.title || "Clawdline", {
        body: notification.body || "",
        tag: notification.tag || "clawdline",
        renotify: true,
        icon: notification.icon || "/icon-128.png",
        data: { url: destination }
    }));
});

self.addEventListener("notificationclick", function (event) {
    var destination = event.notification && event.notification.data
        && event.notification.data.url || "/";
    event.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true })
        .then(function (windows) {
            for (var i = 0; i < windows.length; i += 1) {
                if (windows[i].postMessage) {
                    windows[i].postMessage({ type: "navigate", url: destination });
                }
            }
            if (windows.length && windows[0].focus) return windows[0].focus();
            return self.clients.openWindow(destination);
        }).then(function () {
            // WebKit refuses to close a persistent notification immediately after showing it.
            // Routing the tap comes first; iOS dismisses the notification itself either way.
            try {
                var shown = event.notification && event.notification.timestamp;
                if (typeof shown !== "number" || Date.now() - shown > 1000) {
                    event.notification.close();
                }
            } catch (e) { }
        }));
});
