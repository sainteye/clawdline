/**
 * The Clawdline Chrome extension a connected profile reports (its host's
 * `version`) against the one this console needs: 0.3.0 opens phone tabs in
 * the phone layout and reloads pages. A loaded unpacked extension does not
 * update itself (docs/browser.md), so an older one is shown as needing an
 * update, with the way into its setup guide.
 */
export const WANTED_EXTENSION = "0.3.0"

export function extensionCurrent(version: string | undefined): boolean {
  if (!version) return false
  const have = version.split(".").map((part) => Number.parseInt(part, 10) || 0)
  const want = WANTED_EXTENSION.split(".").map((part) => Number.parseInt(part, 10))
  for (let i = 0; i < want.length; i++) {
    if ((have[i] ?? 0) !== want[i]) return (have[i] ?? 0) > want[i]
  }
  return true
}
