// Fills the explanation from _locales and shows the worker's connection state.
const t = (key) => chrome.i18n.getMessage(key)
document.documentElement.lang = chrome.i18n.getUILanguage()
for (const el of document.querySelectorAll("[data-i18n]")) el.textContent = t(el.dataset.i18n)

chrome.runtime.sendMessage({ type: "status" }).then((s) => {
  const key = !s ? "statusConnecting"
    : s.host === "missing" ? "statusNoHost"
    : s.daemon === "connected" ? "statusReady"
    : s.daemon === "unreachable" ? "statusNoDaemon"
    : "statusConnecting"
  document.getElementById("status").dataset.ok = String(key === "statusReady")
  document.getElementById("status-text").textContent = t(key)
}).catch(() => {})
