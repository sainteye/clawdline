// Clawdline Cloud's service worker. The page is static; this worker exists so an installed PWA
// can own a PushSubscription and wake when the page is closed.
self.addEventListener("install", function () { self.skipWaiting(); });

self.addEventListener("activate", function (event) {
    event.waitUntil(self.clients.claim());
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
