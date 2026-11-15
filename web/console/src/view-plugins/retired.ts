// Withdrawn built-in identities stay reserved so old ZIPs cannot restore them.
export const RETIRED_PACKAGES: readonly string[] = [
  "com.clawdline.session-list", "com.clawdline.session-pop-art", "com.clawdline.session-absurd",
]
export const RETIRED_DOWNLOADS = RETIRED_PACKAGES.map(id => "/view-plugins/" + id.slice("com.clawdline.".length) + ".zip")
