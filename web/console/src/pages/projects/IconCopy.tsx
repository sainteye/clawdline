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
    }).catch(e => { if (active) setError(failureSentence(e, "無法完成圖示操作，請重試。")) })
    return () => { active = false }
  }, [shown])

  function copy() {
    if (!place?.icon) return
    try {
      sessionStorage.setItem(key, JSON.stringify(place.icon))
      setCopied(place.icon as Icon)
      setError("")
      setMessage("已複製圖示。切換到目標機器後，回到 Projects 選擇目標專案並套用。")
    } catch {
      // refusal-ok: only synchronous sessionStorage writes can throw here; no network operation is inside this block.
      setError("瀏覽器無法暫存圖示，請允許此分頁使用儲存空間後重試。")
    }
  }

  async function apply() {
    if (!place?.icon || !copied || busy) return
    setBusy(true); setError(""); setMessage("")
    try {
      const result = await copyProjectIcon(place.id, copied, place.icon)
      setPlaces(rows => rows.map(p => p.id === place.id ? { ...p, icon: result.icon } : p))
      setMessage(`已將圖示儲存到 ${place.label}。`)
      changed()
    } catch (e) {
      setError(e instanceof RefusalError && e.code === "icon_changed"
        ? "目標圖示已變更，請重新讀取並確認後再套用。"
        : failureSentence(e, "無法完成圖示操作，請重試。"))
    }
    finally { setBusy(false) }
  }

  return <details className="project-icon-copy" hidden={!shown}>
    <summary>複製專案圖示</summary>
    <p>在來源機器選擇專案並複製圖示，再切換機器、選擇目標專案並套用。圖示保留在此分頁，直到清除或關閉分頁。</p>
    <label>此機器的專案
      <select value={selected} disabled={busy} onChange={e => { setSelected(e.target.value); setMessage("") }}>
        <option value="">選擇專案</option>
        {places.map(p => <option key={p.id} value={p.id}>{p.label} — {p.path}</option>)}
      </select>
    </label>
    <div className="project-icon-preview">
      {place?.icon ? <span>目前圖示 <Mark icon={place.icon as Icon} cellPx={4} /></span> : null}
      {copied && <span>已複製圖示 <Mark icon={copied} cellPx={4} /></span>}
    </div>
    <div className="project-icon-actions">
      <button type="button" disabled={!place?.icon || busy} onClick={copy}>複製圖示</button>
      <button type="button" disabled={!place?.icon || !copied || busy} onClick={() => void apply()}>{busy ? "儲存中…" : "將已複製圖示套用到此專案"}</button>
      <button type="button" disabled={!copied || busy} onClick={() => {
        try { sessionStorage.removeItem(key); setCopied(null); setMessage("") }
        catch {
          // refusal-ok: this block only removes a browser sessionStorage key, not a remote resource.
          setError("無法清除暫存圖示，請關閉此分頁。")
        }
      }}>清除已複製圖示</button>
      <button type="button" disabled={busy} onClick={() => {
        setSelected(""); setMessage("")
        void readProjectPlaces().then(p => { setPlaces(p.places); setError("") }).catch(e => setError(failureSentence(e, "無法完成圖示操作，請重試。")))
      }}>重新讀取</button>
    </div>
    {message && <p role="status">{message}</p>}
    {error && <p role="alert">{error}</p>}
  </details>
}
