import type { Sources } from "./model.js"
import { supportWord as w } from "./words.js"
function sourceLabel(address: string) {
  try {
    const url = new URL(address), parts = url.pathname.split("/").filter(Boolean)
    const path = url.hostname === "github.com" && parts[2] === "blob" ? parts.slice(4).join("/") : url.hostname === "raw.githubusercontent.com" ? parts.slice(3).join("/") : parts.join("/")
    let decoded = path
    try { decoded = decodeURIComponent(path) } catch { /* Keep the valid URL's encoded spelling. */ }
    return { name: decoded.split("/").at(-1) || url.hostname, path: decoded, href: url.protocol === "https:" ? url.href : null }
  } catch { return { name: address, path: "", href: null } }
}
export function SourceList({ sources }: { sources: Sources }) {
  if (!sources.length) return null
  return <div className="help-sources"><small>{w("sources")}</small>{sources.map(source => {
    const label = sourceLabel(source.url), revision = /^[a-f0-9]{40}$/i.test(source.revision) ? source.revision.slice(0, 7) : source.revision
    const content = <><span className="help-source-file"><span>↗ {label.name}</span>{label.path && label.path !== label.name ? <small>{label.path}</small> : null}</span><code title={source.revision}>{revision}</code></>
    return label.href ? <a className="help-source-link" key={source.url + source.revision} href={label.href} title={source.url + " · " + source.revision} target="_blank" rel="noopener noreferrer">{content}</a> : <span className="help-source-link" key={source.url + source.revision}>{content}</span>
  })}</div>
}
