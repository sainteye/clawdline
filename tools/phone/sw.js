// The service worker behind tools/phone/index.html.
//
// It is not a copy of the one the app ships — that one is in `RemoteServer.serviceWorker()` and
// it is the one a real phone runs. This one exists because a **page cannot see a push event**:
// the event is delivered to a worker, and a worker is the only thing that can be woken when the
// tab is not. So this catches the real push and hands the payload to the page, which draws it.
//
// The payload is passed on **exactly as it arrived**. Nothing here supplies a default title, a
// default body or a fallback string: if the notification in the picture says something, the Mac
// said it. A `||` in this file would be a place for invented copy to get in.

self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (event) { event.waitUntil(self.clients.claim()); });

self.addEventListener("push", function (event) {
    var payload = null;
    try { payload = event.data ? event.data.json() : null; } catch (e) { payload = null; }
    event.waitUntil((async function () {
        var windows = await self.clients.matchAll({ includeUncontrolled: true, type: "window" });
        windows.forEach(function (client) { client.postMessage({ type: "push", payload: payload }); });

        // Chrome requires a subscription made with `userVisibleOnly` to actually show something,
        // and takes the permission away from sites that keep not doing it. So it is shown — and
        // then closed, because the picture being taken is of the phone this page draws, and a
        // second banner from the operating system on top of it is not in the shot. Under
        // `--headless` there is no notification centre to draw it in anyway.
        if (payload) {
            await self.registration.showNotification(payload.title, {
                body: payload.body, tag: payload.tag, data: { url: payload.url }
            });
            var shown = await self.registration.getNotifications();
            shown.forEach(function (n) { n.close(); });
        }
    })());
});
