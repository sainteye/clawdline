// Fills the explanation from _locales and shows whether the worker is connected.
const t = (key, sub) => chrome.i18n.getMessage(key, sub)
document.documentElement.lang = chrome.i18n.getUILanguage()
for (const el of document.querySelectorAll("[data-i18n]")) el.textContent = t(el.dataset.i18n)

chrome.runtime.sendMessage({ type: "status" }).then((s) => {
  const on = !!(s && s.connected)
  document.getElementById("status").dataset.on = String(on)
  document.getElementById("status-text").textContent = t(on ? "statusConnected" : "statusIdle")
  document.getElementById("port").textContent = t("statusPort", [String(s && s.port || 7791)])
}).catch(() => {})
