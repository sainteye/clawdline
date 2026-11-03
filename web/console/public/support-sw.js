// Admin support notifications use their own scope and subscription keys.
self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (event) { event.waitUntil(self.clients.claim()); });
function supportDestination(raw) {
    try {
        var url = new URL(raw, self.location.origin);
        if (url.origin === self.location.origin && !url.username && !url.password && !url.hash &&
            url.pathname === "/admin/health/services" && url.search === "?view=performance") return url.href;
        if (url.origin === self.location.origin && !url.username && !url.password && !url.search && !url.hash &&
            (/^\/help\/admin\/questions\/sq_[a-f0-9]{40}$/.test(url.pathname) || url.pathname === "/help/admin")) return url.href;
    } catch (e) {}
    return self.location.origin + "/help/admin";
}
self.addEventListener("push", function (event) {
    var payload = {};
    try { payload = event.data ? event.data.json() : {}; } catch (e) {}
    var notice = payload.notification || payload;
    var url = supportDestination(notice.navigate);
    event.waitUntil(self.registration.showNotification(notice.title || "Clawdline", {
        body: notice.body || "", tag: notice.tag || "support", icon: "/icon-192.png", data: { url: url }
    }));
});
self.addEventListener("notificationclick", function (event) {
    var url = supportDestination(event.notification && event.notification.data && event.notification.data.url);
    event.notification.close();
    event.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then(function (list) {
        var client = list.find(function (c) { return c.url === url && "focus" in c; });
        if (client) return client.focus();
        // An already open member or Session view must not silently consume the deep link.
        return self.clients.openWindow(url);
    }));
});
