// Clawdline's service worker. Its whole job is to be awake when the page is not.
//
// The worker is served `no-cache`, skips waiting, and claims open pages so an installed
// PWA can move off an old document. Navigations are fetched with `reload`; versioned
// modules and styles stay outside this handler.
self.addEventListener("install", function () { self.skipWaiting(); });

self.addEventListener("activate", function (event) {
    event.waitUntil(
        // Older builds used Cache Storage for notification traces and a second routing
        // record. Declarative Web Push made that record unnecessary, and replaying it
        // could reopen an old Session after the reader returned to the list. Delete every
        // cache left by those builds before claiming their pages.
        caches.keys()
            .then(function (names) {
                return Promise.all(names.map(function (name) {
                    return caches.delete(name);
                }));
            })
            .catch(function () {})
            .then(function () { return self.clients.claim(); })
    );
});

self.addEventListener("fetch", function (event) {
    if (event.request.mode !== "navigate") return;
    event.respondWith(
        fetch(event.request.url, { cache: "reload", credentials: "include" })
            .catch(function () { return fetch(event.request); })
    );
});

self.addEventListener("push", function (event) {
    var payload = {};
    try { payload = event.data ? event.data.json() : {}; } catch (e) {}

    // Declarative Web Push is also a valid legacy payload. Supporting the nested
    // notification here keeps Chrome and older Safari builds working while current
    // WebKit draws and navigates the notification without running this listener.
    var described = (payload.notification && typeof payload.notification === "object")
        ? payload.notification : payload;
    var destination = described.navigate || described.url || payload.url || "/";
    event.waitUntil(self.registration.showNotification(described.title || "Clawdline", {
        body: described.body || "",
        tag: described.tag || "clawdline",
        renotify: true,
        icon: described.icon || "/icon-192.png",
        data: { url: destination }
    }));
});

// Current Apple platforms follow `notification.navigate` declaratively and do not depend
// on this handler. It remains the small compatibility road for browsers that deliver the
// same payload through the legacy Push API.
self.addEventListener("notificationclick", function (event) {
    var notification = event.notification || {};
    var url = (notification.data && notification.data.url) || "/";
    try {
        if (typeof notification.close === "function") notification.close();
    } catch (e) {}

    event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true })
        .then(function (list) {
            for (var i = 0; i < list.length; i++) {
                var client = list[i];
                if (!("focus" in client)) continue;
                if (client.postMessage) {
                    client.postMessage({ type: "navigate", url: url });
                }
                return Promise.resolve(client.focus())
                    .catch(function () {})
                    .then(function () {
                        if (!client.postMessage && client.navigate) {
                            return client.navigate(url).catch(function () {});
                        }
                    });
            }
            return clients.openWindow(url);
        })
        .catch(function () { return clients.openWindow(url); }));
});
