import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { useEffect, useState } from "react"
import { RefusalError } from "@clawdline/core"
import type { Icon } from "@clawdline/contract"
import { Mark } from "../../session/List.js"
import { copyProjectIcon, readProjectPlaces, type ProjectPlace } from "../work/api.js"
import { failureSentence } from "../../legacy/bridge.js"
import "./icon-copy.css"

const key = "clawdline.project-icon-copy.v1"

function savedIcon(): Icon | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) || "null")
    return value && typeof value.accent === "string" && Array.isArray(value.cells) ? value : null
  } catch { return null }
}

// Only the picture survives a machine switch in this tab. The destination is
// chosen afresh from that machine's places; no source path crosses the relay.
export function IconCopy({ shown, changed }: { shown: boolean; changed: () => void }) {
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [selected, setSelected] = useState("")
  const [copied, setCopied] = useState<Icon | null>(savedIcon)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const [error, setError] = useState("")
  const place = places.find(p => p.id === selected)

  useEffect(() => {
    if (!shown) return
    let active = true
    setSelected("")
    setPlaces([])
    void readProjectPlaces().then(answer => {
      if (active) { setPlaces(answer.places); setError("") }
    }).catch(e => { if (active) setError(failureSentence(e, catalogWord("literal", "69ef68c71337"))) })
    return () => { active = false }
  }, [shown])

  function copy() {
    if (!place?.icon) return
    try {
      sessionStorage.setItem(key, JSON.stringify(place.icon))
      setCopied(place.icon as Icon)
      setError("")
      setMessage(catalogWord("literal", "b956e61ad141"))
    } catch {
      // refusal-ok: only synchronous sessionStorage writes can throw here; no network operation is inside this block.
      setError(catalogWord("literal", "2c7adeb9a241"))
    }
  }

  async function apply() {
    if (!place?.icon || !copied || busy) return
    setBusy(true); setError(""); setMessage("")
    try {
      const result = await copyProjectIcon(place.id, copied, place.icon)
      setPlaces(rows => rows.map(p => p.id === place.id ? { ...p, icon: result.icon } : p))
      setMessage(catalogFormat("template", "40fa08f129b0", [place.label]))
      changed()
    } catch (e) {
      setError(e instanceof RefusalError && e.code === "icon_changed"
        ? catalogWord("literal", "67778891527f")
        : failureSentence(e, catalogWord("literal", "69ef68c71337")))
    }
    finally { setBusy(false) }
  }

  return <details className="project-icon-copy" hidden={!shown}>
    <summary>{catalogWord("inline", "d32a04199db7")}</summary>
    <p>{catalogWord("inline", "d3000d0899a0")}</p>
    <label>{catalogWord("inline", "d4a5e9562cbf")}
      <select value={selected} disabled={busy} onChange={e => { setSelected(e.target.value); setMessage("") }}>
        <option value="">{catalogWord("inline", "f7e540512d3c")}</option>
        {places.map(p => <option key={p.id} value={p.id}>{p.label} — {p.path}</option>)}
      </select>
    </label>
    <div className="project-icon-preview">
      {place?.icon ? <span>{catalogWord("inline", "5c552391038f")} <Mark icon={place.icon as Icon} cellPx={4} /></span> : null}
      {copied && <span>{catalogWord("inline", "a6b2e420b2bc")} <Mark icon={copied} cellPx={4} /></span>}
    </div>
    <div className="project-icon-actions">
      <button type="button" disabled={!place?.icon || busy} onClick={copy}>{catalogWord("inline", "4ac77ec01818")}</button>
      <button type="button" disabled={!place?.icon || !copied || busy} onClick={() => void apply()}>{busy ? catalogWord("literal", "9a2a8b2fd5fc") : catalogWord("literal", "05fe9a9feb68")}</button>
      <button type="button" disabled={!copied || busy} onClick={() => {
        try { sessionStorage.removeItem(key); setCopied(null); setMessage("") }
        catch {
          // refusal-ok: this block only removes a browser sessionStorage key, not a remote resource.
          setError(catalogWord("literal", "cc98d5c65273"))
        }
      }}>{catalogWord("inline", "c87eb0f2a589")}</button>
      <button type="button" disabled={busy} onClick={() => {
        setSelected(""); setMessage("")
        void readProjectPlaces().then(p => { setPlaces(p.places); setError("") }).catch(e => setError(failureSentence(e, catalogWord("literal", "69ef68c71337"))))
      }}>{catalogWord("inline", "358a13c304aa")}</button>
    </div>
    {message && <p role="status">{message}</p>}
    {error && <p role="alert">{error}</p>}
  </details>
}
