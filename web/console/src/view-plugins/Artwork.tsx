import galaxy from "../../../../plugins/session-galaxy/manifest.json"
import list from "../../../../plugins/session-list/manifest.json"
import pop from "../../../../plugins/session-pop-art/manifest.json"
import deadpan from "../../../../plugins/session-absurd/manifest.json"
import type { ViewPackage } from "./package.js"

const covers = [
  { manifest: galaxy, legacy: "51770c8f3604165cf352682dbc06b4cfd9c2e8a838a58a1941972478ad887f10" },
  { manifest: list, legacy: "4ededb8ad055fdb6ba76852cfada856feb7ff0da66b5c315cdcf92b2984bac1d" },
  { manifest: pop, legacy: "ccdda46ec99e8715148005d53c7ecb7dd28346f346c67f6eebebf3b044c5270a" },
  { manifest: deadpan },
]
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
