import { ActionProgress, ActionSpinner } from "../ui/ActionProgress.js"
import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from "react"
import { focusModalContainer } from "../modal-focus.js"
import { createPortal } from "react-dom"
import type { SessionRow, TranscriptPage } from "@clawdline/contract"
import { client } from "../client.js"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import { userMessageEntries } from "../legacy/user-messages-bridge.js"
import {
  OPEN_SNIPPETS,
  announceSnippetProject,
  appendToComposer,
  failureSentence,
  rememberSnippetProject,
  snippetActions,
  snippetCreateBody,
  snippetDraft,
  snippetDraftFromText,
  snippetDraftProblem,
  snippetGroups,
  snippetOrder,
  snippetOrderBody,
  snippetPatchBody,
  snippetReorder,
  snippetScopeSwap,
  snippetStarters,
  snippetsListHTML,
  starterPress,
  type Snippet,
  type SnippetAnswer,
  type SnippetDraft,
  type SnippetModel,
} from "../legacy/snippets-bridge.js"
import {
  SNIPPET_CONTROLS,
  createSnippet,
  createPlaybook,
  deletePlaybook,
  deleteSnippet,
  orderSnippets,
  readSnippets,
  readPlaybookProject,
  SnippetFailure,
  updateSnippet,
  updatePlaybook,
} from "./snippets-api.js"
import { VoiceTextarea } from "../pages/work/VoiceTextarea.js"
import { WorkIcon } from "../pages/work/WorkIcon.js"
import { RefusalError } from "@clawdline/core"
import { createDirectTodoV2, directTodoActionV2 } from "../pages/work/api.js"
import { playbookTodoText } from "./playbook-run.js"
import { playbookWord as W } from "../playbook-words.js"
import { readProjectFile } from "../pages/projects/project-files-api.js"
import { playbookSourceMatches, playbookSourceText, playbookSourceTitle } from "../playbook-source.js"
import { ClawdfatherActions, clawdfatherWord } from "./ClawdfatherActions.js"
import { ResourceCoordination } from "./ResourceCoordination.js"
import { catalogWord } from "../catalog.js"
import "../pages/work/work.css"
import "./playbook.css"

/**
 * Snippets — the sheet, and the editor.
 *
 * Text somebody wrote once, pressed instead of typed again. The original is
 * `input/snippets.js`, which builds its own DOM island on the body; the markup
 * below is that module's `install()` string for string, and
 * `legacy/snippets.css` is the copied stylesheet that names it. Everything it
 * decides before writing HTML — which rows this session sees, in which two
 * groups, what each request's body is, which controls may be drawn, and the
 * markup of the list itself — is the copied `view/snippets-data.js`, so nothing
 * about this feature is decided twice.
 *
 * **Pressing a row puts the text in the composer and closes the sheet. It never
 * sends.** That is the whole design rather than a step left out: words in a box
 * that were pressed by a thumb in a pocket are a typo, and the same words
 * already posted to a terminal are an incident. The send button stays the only
 * thing that sends, and `docs/snippets.md` over there records `send_on_tap` as
 * declined rather than postponed.
 *
 * Three deliberate differences from the original, all because this console holds
 * things differently:
 *
 *   - Every paired device can send on this daemon. The copied list renderer
 *     still accepts a historical readOnly option, which this page keeps off.
 *   - The original takes "my last message" from `S.tx.entries`, the copy the
 *     transcript pane already drew. This one reads the transcript once, when the
 *     editor opens for a new snippet, through the same `userMessageEntries` the
 *     我傳出的訊息 sheet uses — so the two cannot disagree about which turn that
 *     is, at the cost of one read nobody waits on.
 *   - Over Clawdline Cloud the list is the whole machine's and names no
 *     project: the wire carries no session (`cloud/carry.ts`), so `snippetGroups`
 *     groups by this session's own `cwd` instead of by the project the Mac
 *     resolved. A session standing in a subdirectory of its project, or in a
 *     worktree, therefore sees its global snippets and an empty project group
 *     where the same session on this machine's own network sees both. A
 *     refusal still lands in the sheet's own note where the list would be, and
 *     the `＋` is not drawn beside an error.
 *
 * **Whose snippets these are.** The machine's. They live in this daemon's own
 * SQLite (`internal/adapters/store/snippets.go`), they are filed against
 * directories that exist on it, and the relay publishes them per machine — so
 * two Macs on one account have two sets and neither is "mine". The sheet says
 * which machine whenever the row it was opened on names one, which is exactly
 * when this console is reading a machine across the relay and more than one
 * could have been meant. On the daemon's own page there is only ever one, and
 * naming it there would be furniture.
 */
/**
 * The sentence for a failure, said by its code and never by the machine's
 * English `message` (`docs/cloud-error-transparency.md` §5 rule 1).
 *
 * `failureSentence` is the copied catalog's, and `cloud_not_carried` is not in
 * it — it is this generation's code, for the Cloud not carrying a route rather
 * than for anything going wrong — so it fell through to "Request failed", and
 * a person opening 常用句 on their phone was told a request had failed and
 * nothing about what to do. This app's own words have a sentence for exactly
 * that (`next-strings.ts`, `cloudNotCarried`), and the Mac is where the list
 * is. The list itself is one of the words this Mac has no route for at all
 * (`cloud/carry.ts`, `NO_MAC_ROUTE`): the seam refuses it here rather than
 * asking a Mac that would answer `unknown_command`.
 */
function snippetSentence(failure: unknown, fallback: string): string {
  const code = (failure as { code?: unknown } | null)?.code
  if (code === "cloud_not_carried") return nextWord("cloudNotCarried")
  return failureSentence(failure, fallback)
}

/**
 * `steward` is Clawdfather's own session. Its sheet gains a first tab of
 * Clawdfather actions and opens on it; Clawdfather stands in no project, so its
 * Snippet and Playbook tabs show the machine-wide groups only.
 */
export function Snippets({ row, steward = false }: { row: SessionRow | null; steward?: boolean }) {
  const [open, setOpen] = useState(false)
  const openedOn = useRef<string | null>(null)

  // A different session, or none, closes the sheet: it is that session's
  // project the list was filtered under (`clawdline:rendered`'s own check).
  useEffect(() => {
    if (!open) return
    if (!row || row.id !== openedOn.current) setOpen(false)
  }, [open, row])

  useEffect(() => {
    const ask = () => {
      if (!row) return
      openedOn.current = row.id
      setOpen(true)
    }
    document.addEventListener(OPEN_SNIPPETS, ask)
    return () => document.removeEventListener(OPEN_SNIPPETS, ask)
  }, [row])

  return createPortal(
    <div className="overlay" id="snippets" hidden={!open} onClick={() => setOpen(false)}>
      {open && row ? <Sheet row={row} steward={steward} onClose={() => setOpen(false)} /> : null}
    </div>,
    document.body,
  )
}

/**
 * The last answer for each session, so an open has something to draw at once.
 *
 * Not storage: a reload asks again, and a session nobody has opened has no
 * entry. A shortcut that spins before it appears is not a shortcut.
 */
const answered: Record<string, SnippetAnswer> = {}

/** Same stretch the transcript pane reads, so "my last message" is the same turn. */
const TRANSCRIPT_LIMIT = 200

type SheetTab = "clawdfather" | "snippet" | "playbook"

function Sheet({ row, steward, onClose }: { row: SessionRow; steward: boolean; onClose: () => void }) {
  const T = L.strings
  const tabs: SheetTab[] = steward ? ["clawdfather", "snippet", "playbook"] : ["snippet", "playbook"]
  const [tab, setTab] = useState<SheetTab>(tabs[0])
  const [coordination, setCoordination] = useState(false)
  const [clawdfatherDraft, setClawdfatherDraft] = useState<{ id: string; title: string; body: string } | null>(null)
  const [clawdfatherSaid, setClawdfatherSaid] = useState("")
  const [clawdfatherArmed, setClawdfatherArmed] = useState(false)
  const [selectedPlaybook, setSelectedPlaybook] = useState<Snippet | null>(null)
  const [startingPlaybook, setStartingPlaybook] = useState(false)
  const [playbookError, setPlaybookError] = useState("")
  const [playbookDraft, setPlaybookDraft] = useState<{ id?: string; title: string; body: string; global: boolean; source?: Snippet["source"] } | null>(null)
  const [playbookSaveError, setPlaybookSaveError] = useState("")
  const [playbookSaving, setPlaybookSaving] = useState(false)
  const [playbookDeleting, setPlaybookDeleting] = useState(false)
  const [playbookSourceStatus, setPlaybookSourceStatus] = useState<"checking" | "ready" | "unavailable" | "scope">("checking")
  const [playbookPreview, setPlaybookPreview] = useState<{ id: string; body: string; name: string; version: string } | null>(null)
  const [projectUnresolved, setProjectUnresolved] = useState(false)
  const [answer, setAnswer] = useState<SnippetAnswer | null>(answered[row.id] ?? null)
  const [loading, setLoading] = useState(!answered[row.id])
  const [error, setError] = useState("")
  const [said, setSaid] = useState("")
  const [menuFor, setMenuFor] = useState(-1)
  const [editing, setEditing] = useState<Snippet | null>(null)
  const [draft, setDraft] = useState<SnippetDraft | null>(null)
  const [editorSaid, setEditorSaid] = useState("")
  const [armed, setArmed] = useState(false)
  const [lastSaid, setLastSaid] = useState("")

  const list = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const sheet = useRef<HTMLDivElement>(null)
  const editorSheet = useRef<HTMLDivElement>(null)
  const clawdfatherSheet = useRef<HTMLDivElement>(null)
  const newButton = useRef<HTMLButtonElement>(null)
  const titleField = useRef<HTMLInputElement>(null)
  // One write at a time: a second ↑ before the first has landed would send an
  // order built from rows the machine has already moved.
  const busy = useRef(false)
  const [writing, setWriting] = useState<"save" | "delete" | "">("")
  // The same for Start: a second tap can land before the disabled button is drawn.
  const launching = useRef(false)
  // Which control the keyboard should be standing on after the next redraw.
  const pendingFocus = useRef<{ attribute: string; id: string; at: number } | null>(null)
  // Only the newest read may paint, the same rule the transcript reads under.
  const reading = useRef(0)
  const alive = useRef(true)
  useEffect(() => () => void (alive.current = false), [])

  const may = snippetActions(SNIPPET_CONTROLS, false)
  // A row that names a machine is a row read across the relay — the local
  // daemon's own `/v1/sessions` carries no such field — and that is the one
  // case where "whose snippets are these" has more than one answer.
  const machine = (row as { machine?: string }).machine ?? null
  const model: SnippetModel = snippetGroups(answer, { machine, project: steward ? null : row.cwd ?? null })
  const whose = machine ? L.machineFor(row).label : ""
  const shown = snippetOrder(model)
  const projectKey = model.project ? model.project.key : ""
  const playbooks = (error ? [] : answer?.snippets ?? []).filter((item) => item.scope === "playbook" &&
    (!item.project || (!projectUnresolved && item.project === projectKey)) &&
    (!projectUnresolved || item.builtin !== "zero_review"))
  const customs = (error ? [] : answer?.snippets ?? []).filter((item) => item.scope === "clawdfather")
    .sort((a, b) => a.position - b.position || a.created_at - b.created_at)
  const currentPlaybook = playbooks.find((item) => item.id === selectedPlaybook?.id) ?? null
  useEffect(() => {
    if (selectedPlaybook && !currentPlaybook) setSelectedPlaybook(null)
  }, [selectedPlaybook, currentPlaybook])

  useEffect(() => {
    const source = currentPlaybook?.source
    setPlaybookPreview(null)
    if (!currentPlaybook || !source) { setPlaybookSourceStatus("ready"); return }
    if (!currentPlaybook.project && source.scope === "project") { setPlaybookSourceStatus("scope"); return }
    let live = true
    setPlaybookSourceStatus("checking")
    readProjectFile(source.place_id, source.file_id).then((answer) => {
      if (!live) return
      if (!playbookSourceMatches({ source }, answer.file)) { setPlaybookSourceStatus("unavailable"); return }
      setPlaybookPreview({ id: currentPlaybook.id, name: playbookSourceTitle(answer.file), version: answer.version,
        body: playbookSourceText(answer.file).body })
      setPlaybookSourceStatus("ready")
    }).catch(() => { if (live) setPlaybookSourceStatus("unavailable") })
    return () => { live = false }
  }, [currentPlaybook])

  const startPlaybook = async () => {
    if (error || !currentPlaybook || (projectUnresolved && (!!currentPlaybook.project || currentPlaybook.builtin === "zero_review")) || launching.current) return
    launching.current = true
    setStartingPlaybook(true); setPlaybookError("")
    let launchBody = currentPlaybook.body
    if (currentPlaybook.source) {
      if (!currentPlaybook.project && currentPlaybook.source.scope === "project") {
        setPlaybookError(W("sourceScopeConflict")); launching.current = false; setStartingPlaybook(false); return
      }
      try {
        const source = currentPlaybook.source
        const answer = await readProjectFile(source.place_id, source.file_id)
        if (!playbookSourceMatches({ source }, answer.file)) {
          setPlaybookError(W("sourceStartUnavailable")); launching.current = false; setStartingPlaybook(false); return
        }
        launchBody = playbookSourceText(answer.file).body
        if (!playbookPreview || playbookPreview.id !== currentPlaybook.id || playbookPreview.body !== launchBody ||
            playbookPreview.name !== playbookSourceTitle(answer.file) || playbookPreview.version !== answer.version) {
          setPlaybookPreview({ id: currentPlaybook.id, name: playbookSourceTitle(answer.file), version: answer.version, body: launchBody })
          setPlaybookError(W("sourceChangedReview")); launching.current = false; setStartingPlaybook(false); return
        }
      } catch { setPlaybookError(W("sourceStartUnavailable")); launching.current = false; setStartingPlaybook(false); return }
    }
    try {
      const text = playbookTodoText(currentPlaybook.id, currentPlaybook.title, launchBody)
      const created = await createDirectTodoV2(row.id, text)
      await directTodoActionV2(row.id, created.todo.id, "send")
      document.dispatchEvent(new CustomEvent("clawdline:playbook-started"))
      onClose()
    } catch (e) {
      // The machine keeps one Playbook running per Session; nothing was written.
      if (e instanceof RefusalError && e.code === "playbook_running") { setPlaybookError(W("alreadyRunning")); return }
      // A created row is durable even when the send was refused or its answer
      // was lost. The to-do fold shows it and offers Send without recreating it.
      document.dispatchEvent(new CustomEvent("clawdline:playbook-started"))
      setPlaybookError(W("startUnknown"))
    } finally { launching.current = false; setStartingPlaybook(false) }
  }
  const editorOpen = !!draft || !!playbookDraft || !!clawdfatherDraft

  const refresh = (options: { keepScroll?: boolean } = {}) => {
    const ticket = ++reading.current
    return readSnippets(row.id)
      .then(async (read) => {
        // Clawdfather's cwd is where it runs, not a project of the person's.
        const next = steward ? { ...read, project: undefined } : read
        if (next.project || !machine || steward) {
          if (alive.current && ticket === reading.current) setProjectUnresolved(false)
          return next
        }
        try {
          const resolved = await readPlaybookProject(row.id)
          if (!resolved.project) throw new Error("missing Project identity")
          if (alive.current && ticket === reading.current) { setProjectUnresolved(false); setPlaybookError("") }
          return { ...next, project: resolved.project }
        } catch {
          // An older machine still serves the Snippet tab. It cannot promise
          // which Project's Playbook belongs in this Session.
          if (alive.current && ticket === reading.current) {
            setPlaybookError(W("projectResolveUnavailable"))
            setProjectUnresolved(true)
          }
          return next
        }
      })
      .then((next) => {
        if (!alive.current || ticket !== reading.current) return
        answered[row.id] = next
        rememberSnippetProject(row.id, next.project)
        announceSnippetProject()
        setAnswer(next)
        setError("")
        setLoading(false)
        if (!options.keepScroll && list.current) list.current.scrollTop = 0
        return true
      })
      .catch((failure: unknown) => {
        if (!alive.current || ticket !== reading.current) return
        setLoading(false)
        // Said by its code, never by the machine's English sentence
        // (`core/failure-text.js`): `cloud_not_carried` is the Cloud not
        // carrying this route, and that is the words a hosted reader gets.
        setError(snippetSentence(failure, T.webRequestFailed))
        return false
      })
  }

  // The sheet is shown first and filled behind: paint what was last answered for
  // this session, then read again.
  useEffect(() => {
    focusModalContainer(sheet.current)
    void refresh({ keepScroll: !!answered[row.id] })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [row.id])

  // Escape, from anywhere: the editor first, because it is over the list, and
  // closing both for one press is what that order prevents. Capture phase, ahead
  // of the page's own document listener, which has no case for these overlays
  // and would close the session behind the sheet.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault()
      event.stopPropagation()
      if (busy.current || playbookSaving || playbookDeleting || startingPlaybook) return
      if (playbookDraft) closePlaybookEditor()
      else if (clawdfatherDraft) closeClawdfatherEditor()
      else if (editorOpen) closeEditor()
      else if (coordination) setCoordination(false)
      else onClose()
    }
    document.addEventListener("keydown", onKey, true)
    return () => document.removeEventListener("keydown", onKey, true)
  })

  /**
   * Where the keyboard goes after a redraw, and why one is needed.
   *
   * Every writing press ends in a redraw of the list's HTML, which destroys the
   * button the reader was standing on. The row is remembered by id, because the
   * redraw is what moves it; a row that is gone leaves the same place in the
   * list as the next best answer, and an empty list falls back to the sheet's
   * own furniture rather than to the page behind it.
   */
  useEffect(() => {
    const wanted = pendingFocus.current
    if (!wanted) return
    pendingFocus.current = null
    let at = shown.findIndex((r) => r && r.id === wanted.id)
    if (at < 0) at = Math.min(wanted.at, shown.length - 1)
    let target: HTMLElement | null = null
    if (at >= 0 && list.current) {
      target =
        list.current.querySelector<HTMLElement>(`[${wanted.attribute}="${at}"]`) ??
        list.current.querySelector<HTMLElement>(`[data-snippet-more="${at}"]`) ??
        list.current.querySelector<HTMLElement>(`[data-snippet="${at}"]`)
    }
    if (!target) target = (may.create ? newButton.current : closeButton.current) ?? null
    target?.focus({ preventScroll: true })
  })

  const wantFocus = (attribute: string, of: Snippet | null) => {
    pendingFocus.current = {
      attribute,
      id: of && typeof of.id === "string" ? of.id : "",
      at: of ? shown.indexOf(of) : -1,
    }
  }

  /**
   * One write, and what the sheet does around it: one at a time, the list re-read
   * afterwards, and a refusal said where the press came from.
   *
   * The list is re-read rather than edited in place because `position` is the
   * machine's to assign — a create lands at the end of its group, a scope change
   * at the end of the other one — and a sheet that guessed those would be showing
   * an order the next reader does not have.
   */
  const write = (work: () => Promise<unknown>, options: { thenClose?: boolean; keepScroll?: boolean; action?: "save" | "delete" } = {}) => {
    if (busy.current) return Promise.resolve(false)
    busy.current = true
    setWriting(options.action ?? "save")
    setSaid("")
    return work()
      .then(() => {
        if (options.thenClose) closeEditor()
        return refresh({ keepScroll: options.keepScroll }).then(() => true)
      })
      .catch((failure: unknown) => {
        // Nothing was redrawn, so the button the press came from still holds
        // the keyboard.
        pendingFocus.current = null
        const message = snippetSentence(failure, T.webRequestFailed)
        if (editorOpen) setEditorSaid(message)
        else setSaid(message)
        return false
      })
      .finally(() => { busy.current = false; setWriting("") })
  }

  /** A press: the words into the box, the sheet shut, and nothing else. */
  const insert = (snippet: Snippet | undefined) => {
    if (!snippet) return
    onClose()
    appendToComposer(snippet.body)
  }

  /**
   * A starter is pressed the way a row is pressed, because it is drawn as one.
   *
   * The words are the visible promise of a press on something that shows its
   * words; keeping the snippet is the useful side effect and belongs behind the
   * press. A save that fails costs nothing the person can see — the text is
   * already in their box and the starter is still on offer next time.
   */
  const useStarter = (at: number) => {
    const starter = snippetStarters()[at]
    if (!starter) return
    const press = starterPress(starter, { mayCreate: may.create })
    if (!press) return
    onClose()
    appendToComposer(press.body)
    if (press.create) void createSnippet(row.id, press.create).catch(() => {})
  }

  const remove = (snippet: Snippet | undefined) => {
    if (!snippet || !may.remove) return
    setMenuFor(-1)
    wantFocus("data-snippet-more", snippet)
    void write(() => deleteSnippet(row.id, snippet.id), { thenClose: true, action: "delete" })
  }

  const move = (snippet: Snippet | undefined, delta: number) => {
    if (!snippet || !may.order) return
    const group = model.groups.find((g) => g.rows.indexOf(snippet) >= 0)
    if (!group) return
    const moved = snippetReorder(group.rows, snippet.id, delta)
    if (!moved) return
    const body = snippetOrderBody(group.scope, projectKey, moved)
    if (!body) return
    // The menu stays open on the row that moved, so a second press is the next
    // step of the same journey rather than three presses to reopen a menu.
    setMenuFor(shown.indexOf(snippet) + delta)
    wantFocus(delta < 0 ? "data-snippet-up" : "data-snippet-down", snippet)
    void write(() => orderSnippets(row.id, body), { keepScroll: true })
  }

  const swapScope = (snippet: Snippet | undefined) => {
    if (!snippet || !may.update) return
    const patch = snippetScopeSwap(snippet, projectKey)
    if (!patch) return
    setMenuFor(-1)
    wantFocus("data-snippet-more", snippet)
    void write(() => updateSnippet(row.id, snippet.id, patch as Record<string, string>))
  }

  const openEditor = (of: Snippet | null) => {
    if (!of && !may.create) return
    setEditing(of)
    setDraft(snippetDraft(of, { scope: "global" }))
    setMenuFor(-1)
    setArmed(false)
    setEditorSaid("")
    setLastSaid("")
    if (!of) {
      // One read, only for a snippet being made, and nothing waits on it: the
      // button it fills appears when the answer arrives.
      void client
        .transcript(row.id, TRANSCRIPT_LIMIT)
        .then((page: TranscriptPage) => {
          if (!alive.current) return
          const mine = userMessageEntries(page.entries, [])
          setLastSaid(mine.length ? String(mine[0].text || "") : "")
        })
        .catch(() => {})
    }
  }

  const closeEditor = () => {
    setEditing(null)
    setDraft(null)
    setEditorSaid("")
    setArmed(false)
    closeButton.current?.focus({ preventScroll: true })
  }

  /** A Clawdfather action, shipped or the person's: its words in the box, nothing sent. */
  const pickClawdfather = (prompt: string) => {
    onClose()
    appendToComposer(prompt)
    document.getElementById("msg")?.focus({ preventScroll: true })
  }

  const openClawdfatherEditor = (of: Snippet | null) => {
    setClawdfatherDraft(of ? { id: of.id, title: of.title, body: of.body } : { id: "", title: "", body: "" })
    setClawdfatherSaid("")
    setClawdfatherArmed(false)
  }

  const closeClawdfatherEditor = () => {
    setClawdfatherDraft(null)
    setClawdfatherSaid("")
    setClawdfatherArmed(false)
    closeButton.current?.focus({ preventScroll: true })
  }

  useLayoutEffect(() => { if (draft) focusModalContainer(editorSheet.current) }, [draft !== null])
  useLayoutEffect(() => { if (clawdfatherDraft) focusModalContainer(clawdfatherSheet.current) }, [clawdfatherDraft !== null])

  const clawdfatherWrite = async (work: () => Promise<unknown>, action: "save" | "delete" = "save") => {
    if (busy.current) return
    busy.current = true
    setWriting(action)
    setClawdfatherSaid("")
    try {
      await work()
      closeClawdfatherEditor()
      await refresh({ keepScroll: true })
    } catch (failure) {
      setClawdfatherSaid(failure instanceof SnippetFailure && failure.code === "snippet_limit_reached"
        ? clawdfatherWord("limitReached") : snippetSentence(failure, T.webRequestFailed))
    } finally { busy.current = false; setWriting("") }
  }

  const saveClawdfather = () => {
    if (!clawdfatherDraft) return
    const title = clawdfatherDraft.title.trim()
    const body = clawdfatherDraft.body
    if (!title || !body.trim()) { setClawdfatherSaid(T.webSnippetNeedsText); return }
    if (new TextEncoder().encode(title).length > 200 || new TextEncoder().encode(body).length > 4000) {
      setClawdfatherSaid(T.webSnippetTooLong); return
    }
    const id = clawdfatherDraft.id
    void clawdfatherWrite(() => id
      ? updateSnippet(row.id, id, { title, body })
      : createSnippet(row.id, { title, body, scope: "clawdfather" }))
  }

  const removeClawdfather = () => {
    if (!clawdfatherDraft?.id) return
    if (!clawdfatherArmed) { setClawdfatherArmed(true); return }
    const id = clawdfatherDraft.id
    void clawdfatherWrite(() => deleteSnippet(row.id, id), "delete")
  }

  const savePlaybook = async () => {
    if (!playbookDraft || playbookSaving) return
    const title = playbookDraft.title.trim()
    const body = playbookDraft.body.trim()
    if (!title || !body) { setPlaybookSaveError(W("needsText")); return }
    if (new TextEncoder().encode(title).length > 200 || new TextEncoder().encode(body).length > 4000) {
      setPlaybookSaveError(W("tooLong")); return
    }
    if (!playbookDraft.id && !playbookDraft.global && (!projectKey || projectUnresolved)) { setPlaybookSaveError(W("projectResolveUnavailable")); return }
    setPlaybookSaving(true); setPlaybookSaveError("")
    try {
      if (playbookDraft.id) await updatePlaybook(machine ?? "", playbookDraft.id, title, body, playbookDraft.source ?? null)
      else await createPlaybook(machine ?? "", playbookDraft.global ? "" : projectKey, title, body)
      if (await refresh({ keepScroll: true })) closePlaybookEditor()
    } catch (cause) {
      setPlaybookSaveError(cause instanceof SnippetFailure && cause.code === "snippet_limit_reached" ? W("limitReached")
        : cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("saveRefused") : W("saveUnknown"))
    } finally { setPlaybookSaving(false) }
  }

  const closePlaybookEditor = () => {
    setPlaybookDraft(null)
    setPlaybookSaveError("")
  }

  const removePlaybook = async () => {
    if (!currentPlaybook || playbookDeleting || startingPlaybook) return
    if (!window.confirm(W("confirmDelete", { name: currentPlaybook.title }))) return
    setPlaybookDeleting(true); setPlaybookError("")
    try {
      await deletePlaybook(machine ?? "", currentPlaybook.id)
      if (await refresh()) setSelectedPlaybook(null)
    } catch (cause) {
      setPlaybookError(cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("deleteRefused") : W("deleteUnknown"))
    } finally { setPlaybookDeleting(false) }
  }

  const save = () => {
    if (!draft) return
    const problem = snippetDraftProblem(draft)
    if (problem) {
      // Three codes and three sentences. `用上一則訊息新增` is how "long" is
      // reached — it assigns a whole message to the body, and a value set in
      // code ignores the textarea's maxlength — and "scope" is unreachable,
      // because the chips are not drawn for a session with no project.
      setEditorSaid(problem === "long" ? T.webSnippetTooLong : T.webSnippetNeedsText)
      return
    }
    if (editing) {
      if (!may.update) return
      const patch = snippetPatchBody(draft, editing)
      if (!patch) return
      // Nothing changed. The store refuses a patch with no fields and is right
      // to; this is a sheet to close rather than a request to make.
      if (!Object.keys(patch).length) {
        closeEditor()
        return
      }
      const id = editing.id
      void write(() => updateSnippet(row.id, id, patch), { thenClose: true })
      return
    }
    if (!may.create) return
    const body = snippetCreateBody(draft)
    if (!body) return
    void write(() => createSnippet(row.id, body), { thenClose: true })
  }

  const rowAt = (target: Element, attribute: string): Snippet | undefined =>
    shown[Number(target.getAttribute(attribute))]

  /**
   * Deleting asks once, in the button itself.
   *
   * A confirmation sheet over an editor over a sheet is three layers deep on a
   * phone. The second press is the confirmation, and any redraw puts the word
   * back — which for the rows happens by construction, because the list's HTML
   * is written again.
   */
  const armInPlace = (target: HTMLElement): boolean => {
    if (target.dataset.armed === "on") return true
    target.dataset.armed = "on"
    target.textContent = T.webSnippetDeleteAsk
    return false
  }

  const pressed = (event: MouseEvent<HTMLDivElement>) => {
    const target = (event.target as Element).closest?.("button") as HTMLElement | null
    if (!target || !list.current?.contains(target)) return
    if (target.hasAttribute("data-snippet-starter")) {
      useStarter(Number(target.getAttribute("data-snippet-starter")))
      return
    }
    if (target.hasAttribute("data-snippet-more")) {
      const at = Number(target.getAttribute("data-snippet-more"))
      wantFocus("data-snippet-more", shown[at])
      setMenuFor(menuFor === at ? -1 : at)
      return
    }
    if (target.hasAttribute("data-snippet-edit")) {
      openEditor(rowAt(target, "data-snippet-edit") ?? null)
      return
    }
    if (target.hasAttribute("data-snippet-delete")) {
      if (!armInPlace(target)) return
      remove(rowAt(target, "data-snippet-delete"))
      return
    }
    if (target.hasAttribute("data-snippet-scope")) {
      swapScope(rowAt(target, "data-snippet-scope"))
      return
    }
    if (target.hasAttribute("data-snippet-up")) {
      move(rowAt(target, "data-snippet-up"), -1)
      return
    }
    if (target.hasAttribute("data-snippet-down")) {
      move(rowAt(target, "data-snippet-down"), 1)
      return
    }
    if (target.classList.contains("snippet-row")) insert(rowAt(target, "data-snippet"))
  }

  const listHTML = snippetsListHTML(model, {
    controls: SNIPPET_CONTROLS,
    readOnly: false,
    loading: false,
    error,
    menuFor,
  })

  return (
    <>
      <div
        className={`sheet snippets-sheet${tab === "playbook" ? " snippets-playbook-sheet" : ""}`}
        ref={sheet}
        id="snippets-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="snippets-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="snippets-head">
          <div className="snippets-heading">
            <h2 id="snippets-title">{tab === "clawdfather" ? (coordination ? catalogWord("resourceCoordination", "title") : catalogWord("literal", "56aed5587136"))
              : tab === "snippet" ? T.webSnippets : W("playbook")}</h2>
            <span className="snippets-where" id="snippets-where">
              {[whose, model.project ? model.project.label : ""].filter(Boolean).join(" · ")}
            </span>
          </div>
          {/* The only writing control outside the list, so the only one this
              file hides by hand; everything else is drawn, or not drawn, by
              `snippetsListHTML`. */}
          <button
            className="snippets-new"
            id="snippets-new"
            type="button" disabled={!!writing}
            ref={newButton}
            hidden={!may.create || loading || !!error || (tab === "playbook" && !!currentPlaybook) || (tab === "clawdfather" && coordination)}
            aria-label={tab === "playbook" ? W("new") : tab === "clawdfather" ? clawdfatherWord("new") : T.webSnippetNew}
            title={tab === "playbook" ? W("new") : tab === "clawdfather" ? clawdfatherWord("new") : T.webSnippetNew}
            onClick={() => tab === "playbook" ? (setPlaybookSaveError(""), setPlaybookDraft({ title: "", body: "", global: !projectKey || projectUnresolved }))
              : tab === "clawdfather" ? openClawdfatherEditor(null) : openEditor(null)}
          >
            ＋
          </button>
        </div>
        {tab === "clawdfather" && coordination ? null : tab === "playbook" && currentPlaybook ? <div className="playbook-detail-nav">
          <button type="button" onClick={() => setSelectedPlaybook(null)}>← {W("backToPlaybooks")}</button>
          <span className="playbook-detail-actions">
            <button type="button" aria-label={W("editAction")} title={W("editAction")} disabled={playbookDeleting || startingPlaybook}
              onClick={() => { setPlaybookSaveError(""); setPlaybookDraft({ id: currentPlaybook.id, title: currentPlaybook.title, body: currentPlaybook.body, global: !currentPlaybook.project, source: currentPlaybook.source }) }}><WorkIcon name="edit" /></button>
            <button type="button" className="playbook-delete" aria-label={playbookDeleting ? catalogWord("literal", "a5df1fce3ed1") : W("delete")} title={playbookDeleting ? catalogWord("literal", "a5df1fce3ed1") : W("delete")} disabled={playbookDeleting || startingPlaybook}
              aria-busy={playbookDeleting} onClick={() => void removePlaybook()}>{playbookDeleting ? <ActionSpinner /> : <WorkIcon name="delete" />}</button>
          </span>
        </div> : <div className="snippets-tabs" role="tablist" aria-label={tabs.map(tabLabel).join(" / ")}
          onKeyDown={(event) => {
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
            event.preventDefault()
            if (busy.current) return
            const next = tabs[(tabs.indexOf(tab) + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length]
            setTab(next)
            ;(event.currentTarget.querySelector(`#${next}-tab`) as HTMLButtonElement | null)?.focus()
          }}>
          {tabs.map((each) => <button key={each} id={`${each}-tab`} type="button" role="tab" aria-controls={tabPanel[each]} tabIndex={tab === each ? 0 : -1}
            aria-selected={tab === each} disabled={!!writing} onClick={() => setTab(each)}>{tabLabel(each)}</button>)}
        </div>}
        {tab === "clawdfather" ? coordination
          ? <ResourceCoordination onBack={() => setCoordination(false)} onAsk={() => pickClawdfather(catalogWord("resourceCoordination", "prompt"))} />
          : <ClawdfatherActions customs={customs} mayEdit={may.update && !error} onPick={pickClawdfather}
            onCoordination={() => setCoordination(true)} onEdit={openClawdfatherEditor} />
        : tab === "snippet" ? <div
          className="snippet-list"
          id="snippet-list"
          role="tabpanel"
          aria-labelledby="snippet-tab"
          ref={list}
          inert={!!writing} aria-busy={!!writing}
          onClick={pressed}
        >{loading ? <SnippetSkeleton label={T.webReading} /> : <div dangerouslySetInnerHTML={{ __html: listHTML }} />}</div> : currentPlaybook ? <div className="playbook-detail-scroll"><div className="playbook-launch">
          <span className="playbook-launch-label">{W("reviewLaunch")}</span><h3>{currentPlaybook.title}</h3><p>{currentPlaybook.source ? playbookPreview?.id === currentPlaybook.id ? playbookPreview.body : W("sourceChecking") : currentPlaybook.body}</p>
          {currentPlaybook.source && <p className="playbook-launch-source">{currentPlaybook.source.kind === "command" ? W("claudeCommand") : "Skill"} · {playbookPreview?.id === currentPlaybook.id ? playbookPreview.name : currentPlaybook.source.name}</p>}
          {currentPlaybook.source && playbookSourceStatus !== "ready" && <p role="status">{W(playbookSourceStatus === "checking" ? "sourceChecking" : playbookSourceStatus === "scope" ? "sourceScopeConflict" : "sourceStartUnavailable")}</p>}
          {playbookError && <p role="alert">{playbookError}</p>}
        </div></div> : <div className="snippet-list playbook-picker" id="playbook-picker" role="tabpanel" aria-labelledby="playbook-tab">
          {loading && <SnippetSkeleton label={W("loading")} />}
          {error && <div className="playbook-picker-error" role="alert"><p>{error}</p>
            <button type="button" onClick={() => { void refresh() }}>{W("reload")}</button></div>}
          {projectUnresolved && !error && <p className="playbook-picker-notice" role="status">{W("projectResolveUnavailable")}</p>}
          {!loading && !error && !projectUnresolved && playbooks.length === 0 && <p className="snippets-note">{W("noSessionPlaybooks")}</p>}
          {([true, false] as const).map((global) => {
            const group = playbooks.filter((item) => !item.project === global)
            if (!group.length) return null
            return <div className="snippet-group playbook-picker-group" key={String(global)}>
              <h3 className="snippet-group-title">{global ? W("global") : W("projectOnly")}</h3>
              {group.map((item) => <button type="button" key={item.id} className="playbook-picker-row"
                onClick={() => { setSelectedPlaybook(item); setPlaybookError("") }}>
                <strong>{item.title}</strong>{item.source && <small>{item.source.kind === "command" ? W("claudeCommand") : "Skill"} · {item.source.name}</small>}<span>{item.source ? W("linkedPickerHint") : item.body}</span>
              </button>)}
            </div>
          })}
        </div>}
        <p className="said" id="snippets-said" role="status" aria-live="polite">
          {writing && !draft && !clawdfatherDraft ? <ActionProgress busy label={catalogWord("literal", writing === "delete" ? "a5df1fce3ed1" : "21aa64dd7446")}>{said}</ActionProgress> : said}
        </p>
        <div className="buttons">
          {tab === "playbook" && <button className="playbook-start" type="button" aria-busy={startingPlaybook} disabled={!currentPlaybook || startingPlaybook || !!currentPlaybook.source && playbookSourceStatus !== "ready"} onClick={() => void startPlaybook()}>
            {startingPlaybook && <span className="playbook-start-spinner" aria-hidden="true" />}
            {startingPlaybook ? W("starting") : W("start")}
          </button>}
          <button className="chip" id="snippets-close" type="button" disabled={!!writing} ref={closeButton} onClick={onClose}>
            {T.webClose}
          </button>
        </div>
      </div>
      {draft ? (
        <div className="overlay" id="snippet-editor" onClick={() => { if (!busy.current) closeEditor() }}>
          <div
            className="sheet snippet-editor-sheet"
            ref={editorSheet}
            id="snippet-editor-sheet"
            role="dialog"
            aria-modal="true"
            aria-labelledby="snippet-editor-title"
            onClick={(event) => event.stopPropagation()}
          >
            <h2 id="snippet-editor-title">{editing ? T.webSnippetEditing : T.webSnippetNew}</h2>
            <div className="block">
              <span className="field-label" id="snippet-title-label">
                {T.webSnippetTitleLabel}
              </span>
              <input
                className="find"
                id="snippet-title"
                type="text"
                maxLength={200}
                autoComplete="off"
                autoCapitalize="sentences"
                spellCheck={false}
                ref={titleField}
                value={draft.title}
                data-1p-ignore=""
                data-lpignore="true"
                data-bwignore=""
                onChange={(event) => {
                  setArmed(false)
                  setDraft({ ...draft, title: event.target.value })
                }}
              />
            </div>
            <div className="block">
              <span className="field-label" id="snippet-body-label">
                {T.webSnippetBodyLabel}
              </span>
              <textarea
                className="find"
                id="snippet-body"
                rows={6}
                maxLength={4000}
                spellCheck={false}
                value={draft.body}
                data-1p-ignore=""
                data-lpignore="true"
                data-bwignore=""
                onChange={(event) => {
                  setArmed(false)
                  setDraft({ ...draft, body: event.target.value })
                }}
              />
              {/* Offered only while there is nothing to overwrite: a button that
                  replaces what somebody has just typed is worse than one they
                  have to look for. */}
              <button
                className="chip snippet-from-last"
                id="snippet-from-last"
                type="button"
                hidden={!!editing || !!draft.body || !lastSaid}
                onClick={() => {
                  if (!lastSaid) return
                  const seeded = snippetDraftFromText(lastSaid)
                  setDraft({ ...draft, title: seeded.title, body: seeded.body })
                }}
              >
                {T.webSnippetFromLast}
              </button>
            </div>
            <div className="block">
              <span className="field-label" id="snippet-scope-label">
                {T.webSnippetScopeLabel}
              </span>
              <div className="row" id="snippet-scope">
                {/* A session whose project the machine did not resolve has no
                    project scope to offer, and a chip that cannot be chosen is
                    not drawn — the rule the rest of this sheet follows. */}
                {(
                  [
                    { scope: "project" as const, label: model.project ? model.project.label : T.webSnippetsThisProject, can: !!projectKey },
                    { scope: "global" as const, label: T.webSnippetsEveryProject, can: true },
                  ] as const
                )
                  .filter((choice) => choice.can)
                  .map((choice) => (
                    <button
                      key={choice.scope}
                      type="button"
                      className={"chip" + (draft.scope === choice.scope ? " on" : "")}
                      data-snippet-scope-choice={choice.scope}
                      aria-pressed={draft.scope === choice.scope}
                      onClick={() =>
                        setDraft({
                          ...draft,
                          scope: choice.scope,
                          project: choice.scope === "project" ? projectKey : "",
                        })
                      }
                    >
                      {choice.label}
                    </button>
                  ))}
              </div>
            </div>
            <p className="said" id="snippet-said" role="status" aria-live="polite">
              {editorSaid}
            </p>
            <div className="buttons">
              <button
                className="chip danger"
                id="snippet-editor-delete"
                type="button" disabled={!!writing} aria-busy={writing === "delete"}
                hidden={!editing || !may.remove}
                onClick={() => {
                  if (!editing) return
                  if (!armed) {
                    setArmed(true)
                    return
                  }
                  remove(editing)
                }}
              >
                <ActionProgress busy={writing === "delete"} label={catalogWord("literal", "a5df1fce3ed1")}>{armed ? T.webSnippetDeleteAsk : T.webSnippetDelete}</ActionProgress>
              </button>
              <button className="chip" id="snippet-editor-cancel" type="button" disabled={!!writing} onClick={closeEditor}>
                {T.webCancel}
              </button>
              <button className="chip confirm-go" id="snippet-editor-save" type="button" disabled={!!writing} aria-busy={writing === "save"} onClick={save}>
                <ActionProgress busy={writing === "save"} label={catalogWord("literal", "21aa64dd7446")}>{T.webSnippetSave}</ActionProgress>
              </button>
            </div>
          </div>
        </div>
      ) : null}
      {clawdfatherDraft && <div className="overlay" id="clawdfather-editor" onClick={() => { if (!busy.current) closeClawdfatherEditor() }}>
        <div className="sheet snippet-editor-sheet" ref={clawdfatherSheet} role="dialog" aria-modal="true" aria-labelledby="clawdfather-editor-title" onClick={(event) => event.stopPropagation()}>
          <h2 id="clawdfather-editor-title">{clawdfatherWord(clawdfatherDraft.id ? "editing" : "new")}</h2>
          <div className="block"><label className="field-label" htmlFor="clawdfather-editor-title-field">{clawdfatherWord("name")}</label>
            <input className="find" id="clawdfather-editor-title-field" type="text" maxLength={200} autoComplete="off" value={clawdfatherDraft.title}
              data-1p-ignore="" data-lpignore="true" data-bwignore=""
              onChange={(event) => { setClawdfatherArmed(false); setClawdfatherDraft({ ...clawdfatherDraft, title: event.target.value }) }} /></div>
          <div className="block"><label className="field-label" htmlFor="clawdfather-editor-body">{clawdfatherWord("message")}</label>
            <textarea className="find" id="clawdfather-editor-body" rows={6} maxLength={4000} spellCheck={false} value={clawdfatherDraft.body}
              data-1p-ignore="" data-lpignore="true" data-bwignore=""
              onChange={(event) => { setClawdfatherArmed(false); setClawdfatherDraft({ ...clawdfatherDraft, body: event.target.value }) }} /></div>
          <p className="said" id="clawdfather-editor-said" role="status" aria-live="polite">{clawdfatherSaid}</p>
          <div className="buttons">
            {clawdfatherDraft.id && may.remove ? <button className="chip danger" id="clawdfather-editor-delete" type="button" disabled={!!writing} aria-busy={writing === "delete"} onClick={removeClawdfather}>
              <ActionProgress busy={writing === "delete"} label={catalogWord("literal", "a5df1fce3ed1")}>{clawdfatherArmed ? T.webSnippetDeleteAsk : T.webSnippetDelete}</ActionProgress></button> : null}
            <button className="chip" type="button" disabled={!!writing} onClick={closeClawdfatherEditor}>{T.webCancel}</button>
            <button className="chip confirm-go" id="clawdfather-editor-save" type="button" disabled={!!writing} aria-busy={writing === "save"} onClick={saveClawdfather}><ActionProgress busy={writing === "save"} label={catalogWord("literal", "21aa64dd7446")}>{T.webSnippetSave}</ActionProgress></button>
          </div>
        </div>
      </div>}
      {playbookDraft && <div className="overlay" id="playbook-quick-editor" onClick={() => { if (!playbookSaving) closePlaybookEditor() }}>
        <div className="sheet snippet-editor-sheet" role="dialog" aria-modal="true" aria-labelledby="playbook-quick-title" onClick={(event) => event.stopPropagation()}>
          <h2 id="playbook-quick-title">{W(playbookDraft.id ? "edit" : "new")}</h2>
          <div className="block"><label className="field-label" htmlFor="playbook-quick-name">{W("name")}</label>
            <input className="find" id="playbook-quick-name" type="text" maxLength={200} value={playbookDraft.title}
              onChange={(event) => setPlaybookDraft({ ...playbookDraft, title: event.target.value })} /></div>
          {playbookDraft.source ? <div className="block"><label className="field-label" htmlFor="playbook-quick-message">{W("message")}</label>
            <textarea className="find" id="playbook-quick-message" rows={6} maxLength={4000} readOnly value={playbookDraft.body} /></div>
            : <div className="block playbook-quick-message"><VoiceTextarea label={W("message")} id="playbook-quick-message" className="find" rows={6} maxLength={4000}
              value={playbookDraft.body} onValue={(body) => setPlaybookDraft((draft) => draft ? { ...draft, body } : draft)} /></div>}
          {playbookDraft.source && <p className="playbook-linked-hint">{W("linkedMessageHint")}</p>}
          {!playbookDraft.id && <div className="block"><span className="field-label">{W("availability")}</span><div className="row">
            {!!projectKey && !projectUnresolved && <button type="button" className={"chip" + (!playbookDraft.global ? " on" : "")}
              aria-pressed={!playbookDraft.global} onClick={() => setPlaybookDraft({ ...playbookDraft, global: false })}>{W("projectOnly")}</button>}
            <button type="button" className={"chip" + (playbookDraft.global ? " on" : "")}
              aria-pressed={playbookDraft.global} onClick={() => setPlaybookDraft({ ...playbookDraft, global: true })}>{W("global")}</button>
          </div></div>}
          {playbookSaveError && <p className="said" role="alert">{playbookSaveError}</p>}
          <div className="buttons"><button className="chip" type="button" disabled={playbookSaving} onClick={closePlaybookEditor}>{W("cancel")}</button>
            <button className="chip confirm-go" type="button" disabled={playbookSaving} aria-busy={playbookSaving} onClick={() => void savePlaybook()}><ActionProgress busy={playbookSaving} label={catalogWord("literal", "21aa64dd7446")}>{W("save")}</ActionProgress></button></div>
        </div>
      </div>}
    </>
  )
}

const tabPanel: Record<SheetTab, string> = { clawdfather: "clawdfather-actions", snippet: "snippet-list", playbook: "playbook-picker" }
const tabLabel = (tab: SheetTab) => tab === "clawdfather" ? "Clawdfather" : tab === "snippet" ? "Snippet" : W("playbook")

function SnippetSkeleton({ label }: { label: string }) {
  return <div className="snippet-skeleton" role="status" aria-label={label}>
    {[0, 1].map((row) => <div className="snippet-skeleton-row" aria-hidden="true" key={row}>
      <span className="snippet-skeleton-title" /><span className="snippet-skeleton-body" />
    </div>)}
  </div>
}
