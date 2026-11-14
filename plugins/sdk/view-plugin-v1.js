/* Clawdline View Plugin v1 SDK. Vendor this file into the self-contained entry.
 * No daemon token, network client, native bridge or automatic retry is provided. */
(() => {
  "use strict";
  const CONNECT_MS = 10000, MAX_PENDING = 16;
  const error = code => Object.assign(new Error(code), { code });
  window.connectViewPlugin = () => new Promise((resolve, reject) => {
    if (window.parent === window) { reject(error("host_unavailable")); return; }
    let connected = false;
    const timeout = setTimeout(() => { window.removeEventListener("message", connect); reject(error("host_unavailable")); }, CONNECT_MS);
    function connect(event) {
      const data = event.data;
      if (event.source !== window.parent || !data || data.type !== "clawdline.plugin.connect" || connected) return;
      if (data.protocolVersion !== 1 || typeof data.instanceId !== "string" || event.ports.length !== 1 || !data.context) return;
      connected = true; clearTimeout(timeout); window.removeEventListener("message", connect);
      const port = event.ports[0], instanceId = data.instanceId, pending = new Map(), contexts = new Set();
      let alive = true, sequence = 0, context = data.context, listener = null;
      const deadline = Math.min(CONNECT_MS, Number(data.limits?.requestMilliseconds) || CONNECT_MS);
      const pendingBound = Math.min(MAX_PENDING, Number(data.limits?.pending) || MAX_PENDING);
      function dispose() {
        if (!alive) return;
        alive = false; port.onmessage = null; port.close(); listener = null; contexts.clear();
        for (const p of pending.values()) { clearTimeout(p.timer); p.reject(error("host_unavailable")); }
        pending.clear();
        window.removeEventListener("pagehide", dispose);
      }
      const call = (method, params) => new Promise((done, refused) => {
        if (!alive) { refused(error("host_unavailable")); return; }
        if (pending.size >= pendingBound) { refused(error("busy")); return; }
        const id = "r" + (++sequence);
        const timer = setTimeout(() => { pending.delete(id); refused(error("request_timeout")); }, deadline);
        pending.set(id, { resolve: done, reject: refused, timer });
        port.postMessage({ v: 1, instanceId, id, method, params });
      });
      port.onmessage = event => {
        const answer = event.data;
        if (!answer || answer.v !== 1 || answer.instanceId !== instanceId || !alive) return;
        if (typeof answer.id === "string") {
          const p = pending.get(answer.id); if (!p) return;
          pending.delete(answer.id); clearTimeout(p.timer);
          if (answer.error && typeof answer.error.code === "string") p.reject(error(answer.error.code));
          else if ("result" in answer) p.resolve(answer.result);
          else p.reject(error("invalid_request"));
        } else if (answer.event === "sessions.snapshot") { listener?.(answer.data); }
        else if (answer.event === "host.context") { context = answer.data; for (const fn of contexts) fn(context); }
      };
      port.onmessageerror = dispose; port.start();
      window.addEventListener("pagehide", dispose);
      resolve({
        sessions: {
          subscribe: async fn => { listener = fn; return call("sessions.subscribe", {}); },
          open: ref => call("sessions.open", { ref }),
        },
        onContext(fn) { contexts.add(fn); fn(context); return () => contexts.delete(fn); },
        close: () => call("view.close", {}),
        dispose,
      });
    }
    window.addEventListener("message", connect);
    window.parent.postMessage({ type: "clawdline.plugin.hello", protocolVersion: 1 }, "*");
  });
})();
