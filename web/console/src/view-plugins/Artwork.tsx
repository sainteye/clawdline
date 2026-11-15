import galaxy from "../../../../plugins/session-galaxy/manifest.json"
import orrery from "../../../../plugins/session-orrery/manifest.json"
import nightCity from "../../../../plugins/session-night-city/manifest.json"
import departures from "../../../../plugins/session-departures/manifest.json"
import territory from "../../../../plugins/session-territory/manifest.json"
import type { ViewPackage } from "./package.js"

const covers = [
  { manifest: orrery },
  { manifest: nightCity },
  { manifest: departures },
  { manifest: territory },
  { manifest: galaxy, legacy: "51770c8f3604165cf352682dbc06b4cfd9c2e8a838a58a1941972478ad887f10" },
]
export const curatedPackageIDs: readonly string[] = covers.map(row => row.manifest.id)
export const exampleCover = (id: string) => covers.find(row => row.manifest.id === id)?.manifest.cover
export function packageCover(pkg: ViewPackage): string | undefined {
  // Attach authored covers to the exact previously shipped archives without replacing code.
  return pkg.manifest.cover ?? covers.find(row => row.manifest.id === pkg.manifest.id && row.legacy === pkg.digest)?.manifest.cover
}

export function ViewPluginIcon({ className }: { className?: string }) {
  return <svg className={className} viewBox="0 0 32 32" fill="none" aria-hidden="true">
    <ellipse cx="16" cy="16" rx="14" ry="6" transform="rotate(-30 16 16)" stroke="currentColor" strokeWidth="1.3" />
    <ellipse cx="16" cy="16" rx="13" ry="6" transform="rotate(40 16 16)" stroke="currentColor" strokeWidth="1" />
    <circle cx="16" cy="16" r="3" fill="currentColor" /><circle cx="27" cy="10" r="2" fill="currentColor" />
  </svg>
}
export function ViewArtwork({ cover, missing, small = false }: { cover?: string; missing: string; small?: boolean }) {
  return <div className={`vp-art${small ? " vp-art-small" : ""}`} aria-hidden="true">
    {cover ? <img src={cover} alt="" decoding="async" /> : <span className="vp-cover-missing">{missing}</span>}
  </div>
}
