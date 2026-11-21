import galaxy from "../../../../plugins/session-galaxy/manifest.json"
import orrery from "../../../../plugins/session-orrery/manifest.json"
import nightCity from "../../../../plugins/session-night-city/manifest.json"
import departures from "../../../../plugins/session-departures/manifest.json"
import territory from "../../../../plugins/session-territory/manifest.json"
import codecraft from "../../../../plugins/session-codecraft/manifest.json"
import kaiju from "../../../../plugins/session-kaiju/manifest.json"
import lanternDeep from "../../../../plugins/session-lantern-deep/manifest.json"
import previews from "./previews.json"
import type { ViewPackage } from "./package.js"

const covers = [
  { manifest: codecraft },
  { manifest: kaiju },
  { manifest: lanternDeep },
  { manifest: orrery },
  { manifest: nightCity },
  { manifest: departures },
  { manifest: territory },
  { manifest: galaxy, legacy: "51770c8f3604165cf352682dbc06b4cfd9c2e8a838a58a1941972478ad887f10" },
]
export const curatedPackageIDs: readonly string[] = covers.map(row => row.manifest.id)
export const examplePreview = (id: string, language: string) => previews.find(row => row.id === id && row.language === (language.startsWith("zh") ? "zh-Hant" : "en"))?.src
export const curatedVersion = (id: string) => covers.find(row => row.manifest.id === id)?.manifest.version
export const exampleCover = (id: string) => covers.find(row => row.manifest.id === id)?.manifest.cover
export function hasCuratedUpdate(pkg: ViewPackage): boolean {
  const offered = covers.find(row => row.manifest.id === pkg.manifest.id)?.manifest.version
  // Curated packages are stable releases. Never offer a downgrade or guess preview ordering.
  if (!offered || !/^[0-9]+\.[0-9]+\.[0-9]+$/.test(offered)) return false
  const installed = pkg.manifest.version.split("+")[0], core = installed.split("-")[0]
  const next = offered.split(".").map(BigInt), prior = core.split(".").map(BigInt)
  for (let i = 0; i < next.length; i++) if (next[i] !== prior[i]) return next[i] > prior[i]
  return installed.includes("-")
}

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
export function ViewArtwork({ cover, missing, small = false, preview = false }: { cover?: string; missing: string; small?: boolean; preview?: boolean }) {
  return <div className={`vp-art${small ? " vp-art-small" : ""}${preview ? " vp-art-preview" : ""}`} aria-hidden="true">
    {cover ? <img src={cover} alt="" decoding="async" /> : <span className="vp-cover-missing">{missing}</span>}
  </div>
}
