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
  createWorkflow,
  deleteWorkflow,
  deleteSnippet,
  orderSnippets,
  readSnippets,
  readWorkflowProject,
  SnippetFailure,
  updateSnippet,
  updateWorkflow,
} from "./snippets-api.js"
import { VoiceTextarea } from "../pages/work/VoiceTextarea.js"
import { WorkIcon } from "../pages/work/WorkIcon.js"
import { RefusalError } from "@clawdline/core"
import { createDirectTodoV2, directTodoActionV2 } from "../pages/work/api.js"
import { workflowTodoText } from "./workflow-run.js"
import { workflowWord as W } from "../workflow-words.js"
import { readProjectFile } from "../pages/projects/project-files-api.js"
import { workflowSourceMatches, workflowSourceText, workflowSourceTitle } from "../workflow-source.js"
import { ClawdfatherActions, clawdfatherWord } from "./ClawdfatherActions.js"
import { ResourceCoordination } from "./ResourceCoordination.js"
import { catalogWord } from "../catalog.js"
import "../pages/work/work.css"
import "./workflow.css"

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
 * Snippet and Workflow tabs show the machine-wide groups only.
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

type SheetTab = "clawdfather" | "snippet" | "workflow"

function Sheet({ row, steward, onClose }: { row: SessionRow; steward: boolean; onClose: () => void }) {
  const T = L.strings
  const tabs: SheetTab[] = steward ? ["clawdfather", "snippet", "workflow"] : ["snippet", "workflow"]
  const [tab, setTab] = useState<SheetTab>(tabs[0])
  const [coordination, setCoordination] = useState(false)
  const [clawdfatherDraft, setClawdfatherDraft] = useState<{ id: string; title: string; body: string } | null>(null)
  const [clawdfatherSaid, setClawdfatherSaid] = useState("")
  const [clawdfatherArmed, setClawdfatherArmed] = useState(false)
  const [selectedWorkflow, setSelectedWorkflow] = useState<Snippet | null>(null)
  const [startingWorkflow, setStartingWorkflow] = useState(false)
  const [workflowError, setWorkflowError] = useState("")
  const [workflowDraft, setWorkflowDraft] = useState<{ id?: string; title: string; body: string; global: boolean; source?: Snippet["source"] } | null>(null)
  const [workflowSaveError, setWorkflowSaveError] = useState("")
  const [workflowSaving, setWorkflowSaving] = useState(false)
  const [workflowDeleting, setWorkflowDeleting] = useState(false)
  const [workflowSourceStatus, setWorkflowSourceStatus] = useState<"checking" | "ready" | "unavailable" | "scope">("checking")
  const [workflowPreview, setWorkflowPreview] = useState<{ id: string; body: string; name: string; version: string } | null>(null)
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
  const workflows = (error ? [] : answer?.snippets ?? []).filter((item) => item.scope === "workflow" &&
    (!item.project || (!projectUnresolved && item.project === projectKey)) &&
    (!projectUnresolved || item.builtin !== "zero_review"))
  const customs = (error ? [] : answer?.snippets ?? []).filter((item) => item.scope === "clawdfather")
    .sort((a, b) => a.position - b.position || a.created_at - b.created_at)
  const currentWorkflow = workflows.find((item) => item.id === selectedWorkflow?.id) ?? null
  useEffect(() => {
    if (selectedWorkflow && !currentWorkflow) setSelectedWorkflow(null)
  }, [selectedWorkflow, currentWorkflow])

  useEffect(() => {
    const source = currentWorkflow?.source
    setWorkflowPreview(null)
    if (!currentWorkflow || !source) { setWorkflowSourceStatus("ready"); return }
    if (!currentWorkflow.project && source.scope === "project") { setWorkflowSourceStatus("scope"); return }
    let live = true
    setWorkflowSourceStatus("checking")
    readProjectFile(source.place_id, source.file_id).then((answer) => {
      if (!live) return
      if (!workflowSourceMatches({ source }, answer.file)) { setWorkflowSourceStatus("unavailable"); return }
      setWorkflowPreview({ id: currentWorkflow.id, name: workflowSourceTitle(answer.file), version: answer.version,
        body: workflowSourceText(answer.file).body })
      setWorkflowSourceStatus("ready")
    }).catch(() => { if (live) setWorkflowSourceStatus("unavailable") })
    return () => { live = false }
  }, [currentWorkflow])

  const startWorkflow = async () => {
    if (error || !currentWorkflow || (projectUnresolved && (!!currentWorkflow.project || currentWorkflow.builtin === "zero_review")) || launching.current) return
    launching.current = true
    setStartingWorkflow(true); setWorkflowError("")
    let launchBody = currentWorkflow.body
    if (currentWorkflow.source) {
      if (!currentWorkflow.project && currentWorkflow.source.scope === "project") {
        setWorkflowError(W("sourceScopeConflict")); launching.current = false; setStartingWorkflow(false); return
      }
      try {
        const source = currentWorkflow.source
        const answer = await readProjectFile(source.place_id, source.file_id)
        if (!workflowSourceMatches({ source }, answer.file)) {
          setWorkflowError(W("sourceStartUnavailable")); launching.current = false; setStartingWorkflow(false); return
        }
        launchBody = workflowSourceText(answer.file).body
        if (!workflowPreview || workflowPreview.id !== currentWorkflow.id || workflowPreview.body !== launchBody ||
            workflowPreview.name !== workflowSourceTitle(answer.file) || workflowPreview.version !== answer.version) {
          setWorkflowPreview({ id: currentWorkflow.id, name: workflowSourceTitle(answer.file), version: answer.version, body: launchBody })
          setWorkflowError(W("sourceChangedReview")); launching.current = false; setStartingWorkflow(false); return
        }
      } catch { setWorkflowError(W("sourceStartUnavailable")); launching.current = false; setStartingWorkflow(false); return }
    }
    try {
      const text = workflowTodoText(currentWorkflow.id, currentWorkflow.title, launchBody)
      const created = await createDirectTodoV2(row.id, text)
      await directTodoActionV2(row.id, created.todo.id, "send")
      document.dispatchEvent(new CustomEvent("clawdline:workflow-started"))
      onClose()
    } catch (e) {
      // The machine keeps one Workflow running per Session; nothing was written.
      if (e instanceof RefusalError && e.code === "workflow_running") { setWorkflowError(W("alreadyRunning")); return }
      // A created row is durable even when the send was refused or its answer
      // was lost. The to-do fold shows it and offers Send without recreating it.
      document.dispatchEvent(new CustomEvent("clawdline:workflow-started"))
      setWorkflowError(W("startUnknown"))
    } finally { launching.current = false; setStartingWorkflow(false) }
  }
  const editorOpen = !!draft || !!workflowDraft || !!clawdfatherDraft

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
          const resolved = await readWorkflowProject(row.id)
          if (!resolved.project) throw new Error("missing Project identity")
          if (alive.current && ticket === reading.current) { setProjectUnresolved(false); setWorkflowError("") }
          return { ...next, project: resolved.project }
        } catch {
          // An older machine still serves the Snippet tab. It cannot promise
          // which Project's Workflow belongs in this Session.
          if (alive.current && ticket === reading.current) {
            setWorkflowError(W("projectResolveUnavailable"))
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
      if (busy.current || workflowSaving || workflowDeleting || startingWorkflow) return
      if (workflowDraft) closeWorkflowEditor()
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

  const saveWorkflow = async () => {
    if (!workflowDraft || workflowSaving) return
    const title = workflowDraft.title.trim()
    const body = workflowDraft.body.trim()
    if (!title || !body) { setWorkflowSaveError(W("needsText")); return }
    if (new TextEncoder().encode(title).length > 200 || new TextEncoder().encode(body).length > 4000) {
      setWorkflowSaveError(W("tooLong")); return
    }
    if (!workflowDraft.id && !workflowDraft.global && (!projectKey || projectUnresolved)) { setWorkflowSaveError(W("projectResolveUnavailable")); return }
    setWorkflowSaving(true); setWorkflowSaveError("")
    try {
      if (workflowDraft.id) await updateWorkflow(machine ?? "", workflowDraft.id, title, body, workflowDraft.source ?? null)
      else await createWorkflow(machine ?? "", workflowDraft.global ? "" : projectKey, title, body)
      if (await refresh({ keepScroll: true })) closeWorkflowEditor()
    } catch (cause) {
      setWorkflowSaveError(cause instanceof SnippetFailure && cause.code === "snippet_limit_reached" ? W("limitReached")
        : cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("saveRefused") : W("saveUnknown"))
    } finally { setWorkflowSaving(false) }
  }

  const closeWorkflowEditor = () => {
    setWorkflowDraft(null)
    setWorkflowSaveError("")
  }

  const removeWorkflow = async () => {
    if (!currentWorkflow || workflowDeleting || startingWorkflow) return
    if (!window.confirm(W("confirmDelete", { name: currentWorkflow.title }))) return
    setWorkflowDeleting(true); setWorkflowError("")
    try {
      await deleteWorkflow(machine ?? "", currentWorkflow.id)
      if (await refresh()) setSelectedWorkflow(null)
    } catch (cause) {
      setWorkflowError(cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("deleteRefused") : W("deleteUnknown"))
    } finally { setWorkflowDeleting(false) }
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
        className={`sheet snippets-sheet${tab === "workflow" ? " snippets-workflow-sheet" : ""}`}
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
              : tab === "snippet" ? T.webSnippets : W("workflow")}</h2>
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
            hidden={!may.create || loading || !!error || (tab === "workflow" && !!currentWorkflow) || (tab === "clawdfather" && coordination)}
            aria-label={tab === "workflow" ? W("new") : tab === "clawdfather" ? clawdfatherWord("new") : T.webSnippetNew}
            title={tab === "workflow" ? W("new") : tab === "clawdfather" ? clawdfatherWord("new") : T.webSnippetNew}
            onClick={() => tab === "workflow" ? (setWorkflowSaveError(""), setWorkflowDraft({ title: "", body: "", global: !projectKey || projectUnresolved }))
              : tab === "clawdfather" ? openClawdfatherEditor(null) : openEditor(null)}
          >
            ＋
          </button>
        </div>
        {tab === "clawdfather" && coordination ? null : tab === "workflow" && currentWorkflow ? <div className="workflow-detail-nav">
          <button type="button" onClick={() => setSelectedWorkflow(null)}>← {W("backToWorkflows")}</button>
          <span className="workflow-detail-actions">
            <button type="button" aria-label={W("editAction")} title={W("editAction")} disabled={workflowDeleting || startingWorkflow}
              onClick={() => { setWorkflowSaveError(""); setWorkflowDraft({ id: currentWorkflow.id, title: currentWorkflow.title, body: currentWorkflow.body, global: !currentWorkflow.project, source: currentWorkflow.source }) }}><WorkIcon name="edit" /></button>
            <button type="button" className="workflow-delete" aria-label={workflowDeleting ? catalogWord("literal", "a5df1fce3ed1") : W("delete")} title={workflowDeleting ? catalogWord("literal", "a5df1fce3ed1") : W("delete")} disabled={workflowDeleting || startingWorkflow}
              aria-busy={workflowDeleting} onClick={() => void removeWorkflow()}>{workflowDeleting ? <ActionSpinner /> : <WorkIcon name="delete" />}</button>
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
        >{loading ? <SnippetSkeleton label={T.webReading} /> : <div dangerouslySetInnerHTML={{ __html: listHTML }} />}</div> : currentWorkflow ? <div className="workflow-detail-scroll"><div className="workflow-launch">
          <span className="workflow-launch-label">{W("reviewLaunch")}</span><h3>{currentWorkflow.title}</h3><p>{currentWorkflow.source ? workflowPreview?.id === currentWorkflow.id ? workflowPreview.body : W("sourceChecking") : currentWorkflow.body}</p>
          {currentWorkflow.source && <p className="workflow-launch-source">{currentWorkflow.source.kind === "command" ? W("claudeCommand") : "Skill"} · {workflowPreview?.id === currentWorkflow.id ? workflowPreview.name : currentWorkflow.source.name}</p>}
          {currentWorkflow.source && workflowSourceStatus !== "ready" && <p role="status">{W(workflowSourceStatus === "checking" ? "sourceChecking" : workflowSourceStatus === "scope" ? "sourceScopeConflict" : "sourceStartUnavailable")}</p>}
          {workflowError && <p role="alert">{workflowError}</p>}
        </div></div> : <div className="snippet-list workflow-picker" id="workflow-picker" role="tabpanel" aria-labelledby="workflow-tab">
          {loading && <SnippetSkeleton label={W("loading")} />}
          {error && <div className="workflow-picker-error" role="alert"><p>{error}</p>
            <button type="button" onClick={() => { void refresh() }}>{W("reload")}</button></div>}
          {projectUnresolved && !error && <p className="workflow-picker-notice" role="status">{W("projectResolveUnavailable")}</p>}
          {!loading && !error && !projectUnresolved && workflows.length === 0 && <p className="snippets-note">{W("noSessionWorkflows")}</p>}
          {([true, false] as const).map((global) => {
            const group = workflows.filter((item) => !item.project === global)
            if (!group.length) return null
            return <div className="snippet-group workflow-picker-group" key={String(global)}>
              <h3 className="snippet-group-title">{global ? W("global") : W("projectOnly")}</h3>
              {group.map((item) => <button type="button" key={item.id} className="workflow-picker-row"
                onClick={() => { setSelectedWorkflow(item); setWorkflowError("") }}>
                <strong>{item.title}</strong>{item.source && <small>{item.source.kind === "command" ? W("claudeCommand") : "Skill"} · {item.source.name}</small>}<span>{item.source ? W("linkedPickerHint") : item.body}</span>
              </button>)}
            </div>
          })}
        </div>}
        <p className="said" id="snippets-said" role="status" aria-live="polite">
          {writing && !draft && !clawdfatherDraft ? <ActionProgress busy label={catalogWord("literal", writing === "delete" ? "a5df1fce3ed1" : "21aa64dd7446")}>{said}</ActionProgress> : said}
        </p>
        <div className="buttons">
          {tab === "workflow" && <button className="workflow-start" type="button" aria-busy={startingWorkflow} disabled={!currentWorkflow || startingWorkflow || !!currentWorkflow.source && workflowSourceStatus !== "ready"} onClick={() => void startWorkflow()}>
            {startingWorkflow && <span className="workflow-start-spinner" aria-hidden="true" />}
            {startingWorkflow ? W("starting") : W("start")}
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
      {workflowDraft && <div className="overlay" id="workflow-quick-editor" onClick={() => { if (!workflowSaving) closeWorkflowEditor() }}>
        <div className="sheet snippet-editor-sheet" role="dialog" aria-modal="true" aria-labelledby="workflow-quick-title" onClick={(event) => event.stopPropagation()}>
          <h2 id="workflow-quick-title">{W(workflowDraft.id ? "edit" : "new")}</h2>
          <div className="block"><label className="field-label" htmlFor="workflow-quick-name">{W("name")}</label>
            <input className="find" id="workflow-quick-name" type="text" maxLength={200} value={workflowDraft.title}
              onChange={(event) => setWorkflowDraft({ ...workflowDraft, title: event.target.value })} /></div>
          {workflowDraft.source ? <div className="block"><label className="field-label" htmlFor="workflow-quick-message">{W("message")}</label>
            <textarea className="find" id="workflow-quick-message" rows={6} maxLength={4000} readOnly value={workflowDraft.body} /></div>
            : <div className="block workflow-quick-message"><VoiceTextarea label={W("message")} id="workflow-quick-message" className="find" rows={6} maxLength={4000}
              value={workflowDraft.body} onValue={(body) => setWorkflowDraft((draft) => draft ? { ...draft, body } : draft)} /></div>}
          {workflowDraft.source && <p className="workflow-linked-hint">{W("linkedMessageHint")}</p>}
          {!workflowDraft.id && <div className="block"><span className="field-label">{W("availability")}</span><div className="row">
            {!!projectKey && !projectUnresolved && <button type="button" className={"chip" + (!workflowDraft.global ? " on" : "")}
              aria-pressed={!workflowDraft.global} onClick={() => setWorkflowDraft({ ...workflowDraft, global: false })}>{W("projectOnly")}</button>}
            <button type="button" className={"chip" + (workflowDraft.global ? " on" : "")}
              aria-pressed={workflowDraft.global} onClick={() => setWorkflowDraft({ ...workflowDraft, global: true })}>{W("global")}</button>
          </div></div>}
          {workflowSaveError && <p className="said" role="alert">{workflowSaveError}</p>}
          <div className="buttons"><button className="chip" type="button" disabled={workflowSaving} onClick={closeWorkflowEditor}>{W("cancel")}</button>
            <button className="chip confirm-go" type="button" disabled={workflowSaving} aria-busy={workflowSaving} onClick={() => void saveWorkflow()}><ActionProgress busy={workflowSaving} label={catalogWord("literal", "21aa64dd7446")}>{W("save")}</ActionProgress></button></div>
        </div>
      </div>}
    </>
  )
}

const tabPanel: Record<SheetTab, string> = { clawdfather: "clawdfather-actions", snippet: "snippet-list", workflow: "workflow-picker" }
const tabLabel = (tab: SheetTab) => tab === "clawdfather" ? "Clawdfather" : tab === "snippet" ? "Snippet" : W("workflow")

function SnippetSkeleton({ label }: { label: string }) {
  return <div className="snippet-skeleton" role="status" aria-label={label}>
    {[0, 1].map((row) => <div className="snippet-skeleton-row" aria-hidden="true" key={row}>
      <span className="snippet-skeleton-title" /><span className="snippet-skeleton-body" />
    </div>)}
  </div>
}
