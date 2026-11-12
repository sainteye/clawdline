// Test-only: holds chrome.debugger on every tab that finishes loading, so the
// probe can measure what a second debugging extension meets on that tab.
chrome.tabs.onUpdated.addListener((tabId, info) => {
  if (info.status !== "complete") return
  chrome.debugger.attach({ tabId }, "1.3").catch(() => {})
})
