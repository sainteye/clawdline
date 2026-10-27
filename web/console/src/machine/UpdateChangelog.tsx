import { useEffect, useId, useRef, useState } from "react"
import type { UpdateStatus } from "@clawdline/contract"
import { onMachineChanged } from "../cloud/machine-switch.js"
import { nextWord } from "../next-strings.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { failureSentence } from "../refusals/refusal-text.js"
import { richText } from "../legacy/js/view/markdown.js"
import { buildName, canStartUpdateAfterFresh } from "./update-model.js"
import { checkUpdateNow, currentUpdateRead } from "./update.js"
import "./update-changelog.css"

/** Public release copy is informational; only the daemon's signed check authorizes installation. */
export async function releaseChangelog(version: string, signal: AbortSignal): Promise<string> {
  const response = await fetch("https://api.github.com/repos/sainteye/clawdline/releases/tags/" + encodeURIComponent(version), {
    signal, credentials: "omit", headers: { Accept: "application/vnd.github+json" },
  })
  if (!response.ok) throw new Error("release_notes_unavailable")
  const release = await response.json()
  if (release.tag_name !== version || release.draft || typeof release.body !== "string" || !release.body.trim()) {
    throw new Error("release_notes_unavailable")
  }
  return release.body
}

/** Mounted only after a manual update press; opening or dismissing never installs anything. */
export function UpdateChangelog({ latest, onClose, onConfirm }: {
  latest: UpdateStatus["latest"]
  onClose: () => void
  onConfirm: () => void
}) {
  const [reviewed, setReviewed] = useState(latest)
  const [notes, setNotes] = useState<string | null>(null)
  const [failed, setFailed] = useState("")
  const [attempt, setAttempt] = useState(0)
  const [checking, setChecking] = useState(false)
  const [notice, setNotice] = useState("")
  const dialog = useRef<HTMLDialogElement>(null)
  const active = useRef(true)
  const confirming = useRef(false)
  const title = useId()
  const version = buildName(reviewed)

  useEffect(() => {
    active.current = true
    const opener = document.activeElement as HTMLElement | null
    showModalWithoutChoice(dialog.current)
    const stop = onMachineChanged(() => { active.current = false; onClose() })
    return () => { active.current = false; stop(); opener?.focus() }
  }, [onClose])

  useEffect(() => {
    const controller = new AbortController()
    setNotes(null)
    setFailed("")
    void releaseChangelog(version, controller.signal).then(
      text => { if (!controller.signal.aborted) setNotes(text) },
      (error: unknown) => { if (!controller.signal.aborted) setFailed(failureSentence(error, nextWord("updateChangelogFailed"))) },
    )
    return () => controller.abort()
  }, [version, attempt])

  const confirm = async () => {
    if (confirming.current || (!notes && !failed)) return
    confirming.current = true
    setChecking(true)
    const checked = await checkUpdateNow()
    if (!active.current) return
    confirming.current = false
    setChecking(false)
    if (!canStartUpdateAfterFresh(checked, currentUpdateRead()) || checked.kind !== "status") {
      setNotice(nextWord("updateIsUnknown"))
      return
    }
    const fresh = checked.status.latest
    if (fresh.version !== reviewed.version || fresh.stamp !== reviewed.stamp) {
      setReviewed(fresh)
      setNotes(null)
      setNotice(nextWord("updateChangelogChanged"))
      // Even a replaced manifest with the same tag needs another explicit confirmation.
      setAttempt(value => value + 1)
      return
    }
    onConfirm()
    onClose()
  }

  return <dialog ref={dialog} className="update-changelog" aria-labelledby={title}
    onCancel={event => { event.preventDefault(); onClose() }}>
    <h2 id={title}>{nextWord("updateChangelogTitle", { version })}</h2>
    <div className="update-changelog-body" aria-busy={!notes && !failed}>
      {notes ? <div className="update-changelog-copy" dangerouslySetInnerHTML={{ __html: richText(notes) }} />
        : <p role="status">{failed || nextWord("updateChangelogLoading")}</p>}
      {failed && <button className="chip" type="button" onClick={() => setAttempt(value => value + 1)}>{nextWord("updateChangelogRetry")}</button>}
    </div>
    <a href={"https://github.com/sainteye/clawdline/releases/tag/" + encodeURIComponent(version)} target="_blank" rel="noopener noreferrer">{nextWord("updateChangelogOriginal")}</a>
    {notice && <p className="update-changelog-notice" role="status">{notice}</p>}
    <div className="update-changelog-buttons">
      <button className="chip" type="button" onClick={onClose}>{nextWord("updateChangelogLater")}</button>
      <button className="chip primary" type="button" disabled={checking || (!notes && !failed)} onClick={() => void confirm()}>
        {nextWord(checking ? "updateChangelogChecking" : failed ? "updateChangelogAnyway" : "updateChangelogConfirm")}
      </button>
    </div>
  </dialog>
}
