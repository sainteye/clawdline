import { useRef, useState } from "react"
import { freshRoleIcon, RoleIconEditor } from "./RoleIconEditor.js"
import { nextWord } from "../../next-strings.js"
import { showModalWithoutChoice } from "../../modal-focus.js"
import { failureSentence } from "../../refusals/refusal-text.js"
import type { SquadAPI } from "./api.js"

function freshID(): string { return `personal.persona.role-${crypto.randomUUID().replaceAll("-", "").slice(0, 12)}` }

export function PersonaCreator({ api, canWrite, catalogVersion, onSaved }: {
  api: SquadAPI
  canWrite: boolean
  catalogVersion: number
  onSaved: (id: string) => Promise<void>
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLButtonElement>(null)
  const [icon, setIcon] = useState(freshRoleIcon)
  const [id, setID] = useState(freshID)
  const [description, setDescription] = useState("")
  const [name, setName] = useState("")
  const [summary, setSummary] = useState("")
  const [body, setBody] = useState("")
  const [busy, setBusy] = useState<"draft" | "save" | "">("")
  const [error, setError] = useState("")

  const close = () => { if (!busy) dialog.current?.close() }
  const draft = async () => {
    if (!description.trim() || busy) return
    setBusy("draft"); setError("")
    try {
      const result = await api.draftRole(description.trim(), crypto.randomUUID())
      setName(result.name); setSummary(result.summary); setBody(result.body)
    } catch (cause) { setError(failureSentence(cause, nextWord("roleCreateFailed"))) }
    finally { setBusy("") }
  }
  const save = async () => {
    if (busy || !canWrite) return
    if (!/^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$/.test(id) || id.startsWith("clawdline.") || !name.trim() || !summary.trim() || !body.trim()) {
      setError(nextWord("roleCreateRequired")); return
    }
    setBusy("save"); setError("")
    try {
      await api.createPersona({
        definition_id: id, version: "1.0.0", source: "Created by the person in Clawdline", license: "UNLICENSED",
        name: { en: name.trim(), "zh-Hant": name.trim() },
        summary: { en: summary.trim(), "zh-Hant": summary.trim() },
        body: body.trim(), icon, teams: [{ id: "clawdline.team.custom", version: "1" }], skills: [],
      }, catalogVersion, crypto.randomUUID())
      dialog.current?.close()
      await onSaved(id)
      setIcon(freshRoleIcon()); setID(freshID()); setDescription(""); setName(""); setSummary(""); setBody("")
    } catch (cause) { setError(failureSentence(cause, nextWord("roleCreateFailed"))) }
    finally { setBusy("") }
  }

  return <>
    <button ref={opener} type="button" disabled={!canWrite} onClick={() => { setError(""); showModalWithoutChoice(dialog.current) }}>{nextWord("roleCreateOpen")}</button>
    <dialog data-modal-dismiss={String(!busy)} ref={dialog} className="squad-dialog squad-role-create" aria-labelledby="squad-role-create-title" onClose={() => opener.current?.focus()} onCancel={(event) => { if (busy) event.preventDefault() }}>
      <div className="squad-dialog-head"><h2 id="squad-role-create-title">{nextWord("roleCreateTitle")}</h2><button type="button" aria-label={nextWord("roleCreateClose")} disabled={!!busy} onClick={close}>×</button></div>
      <p>{nextWord("roleCreateIntro")}</p>
      <section className="squad-role-ai" aria-labelledby="squad-role-ai-title">
        <h3 id="squad-role-ai-title">{nextWord("roleCreateAI")}</h3>
        <label>{nextWord("roleCreateDescribe")}<textarea value={description} maxLength={4000} rows={3} disabled={!!busy} onChange={(event) => setDescription(event.target.value)} /></label>
        <p>{nextWord("roleCreateUsage")}</p>
        <button type="button" disabled={!canWrite || !!busy || !description.trim()} onClick={() => void draft()}>{busy === "draft" ? nextWord("roleCreateDrafting") : nextWord("roleCreateGenerate")}</button>
      </section>
      <form className="squad-role-form" onSubmit={(event) => { event.preventDefault(); void save() }}>
        <h3>{nextWord("roleCreateReview")}</h3>
        <RoleIconEditor icon={icon} onChange={setIcon} disabled={!!busy} />
        <label>{nextWord("roleCreateName")}<input required maxLength={120} value={name} disabled={!!busy} onChange={(event) => setName(event.target.value)} /></label>
        <label>{nextWord("roleCreateSummary")}<textarea required maxLength={500} rows={2} value={summary} disabled={!!busy} onChange={(event) => setSummary(event.target.value)} /></label>
        <label>{nextWord("roleCreateDefinition")}<textarea required maxLength={6000} rows={12} value={body} disabled={!!busy} onChange={(event) => setBody(event.target.value)} /></label>
        <label>{nextWord("roleCreateID")}<input required value={id} disabled={!!busy} onChange={(event) => setID(event.target.value)} /></label>
        <p>{nextWord("roleCreateIDHint")}</p>
        {error && <p className="squad-dialog-error" role="alert">{error}</p>}
        <div className="squad-actions"><button type="submit" disabled={!canWrite || !!busy}>{busy === "save" ? nextWord("roleCreateSaving") : nextWord("roleCreateSave")}</button><button type="button" disabled={!!busy} onClick={close}>{nextWord("roleCreateClose")}</button></div>
      </form>
    </dialog>
  </>
}
