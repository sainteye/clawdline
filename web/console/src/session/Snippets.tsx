import { useEffect, useRef, useState, type MouseEvent } from "react"
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
  deleteSnippet,
  orderSnippets,
  readSnippets,
  updateSnippet,
} from "./snippets-api.js"

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
 *   - The original reads `S.write` to know whether this device may write, and
 *     draws a read-only list when it may not. This console has no capabilities
 *     reading yet, so a device that may only read is told so by the refusal's
 *     own code (`forbidden`, which the catalog has a sentence for) rather than
 *     by a control that was never drawn. `readOnly` below is the one place such
 *     a reading would go.
 *   - The original takes "my last message" from `S.tx.entries`, the copy the
 *     transcript pane already drew. This one reads the transcript once, when the
 *     editor opens for a new snippet, through the same `userMessageEntries` the
 *     我傳出的訊息 sheet uses — so the two cannot disagree about which turn that
 *     is, at the cost of one read nobody waits on.
 *   - The Cloud does not carry these routes. The relay refuses the read by name
 *     (`cloud_not_carried`), which lands in the sheet's own note where the list
 *     would be, and the `＋` is not drawn beside an error — so a hosted console
 *     says what it cannot do instead of showing an empty list.
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

export function Snippets({ row }: { row: SessionRow | null }) {
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
      {open && row ? <Sheet row={row} onClose={() => setOpen(false)} /> : null}
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

function Sheet({ row, onClose }: { row: SessionRow; onClose: () => void }) {
  const T = L.strings
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
  const newButton = useRef<HTMLButtonElement>(null)
  const titleField = useRef<HTMLInputElement>(null)
  // One write at a time: a second ↑ before the first has landed would send an
  // order built from rows the machine has already moved.
  const busy = useRef(false)
  // Which control the keyboard should be standing on after the next redraw.
  const pendingFocus = useRef<{ attribute: string; id: string; at: number } | null>(null)
  // Only the newest read may paint, the same rule the transcript reads under.
  const reading = useRef(0)
  const alive = useRef(true)
  useEffect(() => () => void (alive.current = false), [])

  const may = snippetActions(SNIPPET_CONTROLS, false)
  const model: SnippetModel = snippetGroups(answer, {
    machine: (row as { machine?: string }).machine ?? null,
    project: row.cwd ?? null,
  })
  const shown = snippetOrder(model)
  const projectKey = model.project ? model.project.key : ""
  const editorOpen = !!draft

  const refresh = (options: { keepScroll?: boolean } = {}) => {
    const ticket = ++reading.current
    return readSnippets(row.id)
      .then((next) => {
        if (!alive.current || ticket !== reading.current) return
        answered[row.id] = next
        rememberSnippetProject(row.id, next.project)
        announceSnippetProject()
        setAnswer(next)
        setError("")
        setLoading(false)
        if (!options.keepScroll && list.current) list.current.scrollTop = 0
      })
      .catch((failure: unknown) => {
        if (!alive.current || ticket !== reading.current) return
        setLoading(false)
        // Said by its code, never by the machine's English sentence
        // (`core/failure-text.js`): `cloud_not_carried` is the Cloud not
        // carrying this route, and that is the words a hosted reader gets.
        setError(snippetSentence(failure, T.webRequestFailed))
      })
  }

  // The sheet is shown first and filled behind: paint what was last answered for
  // this session, then read again.
  useEffect(() => {
    closeButton.current?.focus({ preventScroll: true })
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
      if (editorOpen) closeEditor()
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
  const write = (work: () => Promise<unknown>, options: { thenClose?: boolean; keepScroll?: boolean } = {}) => {
    if (busy.current) return Promise.resolve(false)
    busy.current = true
    setSaid("")
    return work()
      .then(() => {
        busy.current = false
        if (options.thenClose) closeEditor()
        return refresh({ keepScroll: options.keepScroll }).then(() => true)
      })
      .catch((failure: unknown) => {
        busy.current = false
        // Nothing was redrawn, so the button the press came from still holds
        // the keyboard.
        pendingFocus.current = null
        const message = snippetSentence(failure, T.webRequestFailed)
        if (editorOpen) setEditorSaid(message)
        else setSaid(message)
        return false
      })
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
    if (press.create) void createSnippet(press.create).catch(() => {})
  }

  const remove = (snippet: Snippet | undefined) => {
    if (!snippet || !may.remove) return
    setMenuFor(-1)
    wantFocus("data-snippet-more", snippet)
    void write(() => deleteSnippet(snippet.id), { thenClose: true })
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
    void write(() => orderSnippets(body), { keepScroll: true })
  }

  const swapScope = (snippet: Snippet | undefined) => {
    if (!snippet || !may.update) return
    const patch = snippetScopeSwap(snippet, projectKey)
    if (!patch) return
    setMenuFor(-1)
    wantFocus("data-snippet-more", snippet)
    void write(() => updateSnippet(snippet.id, patch as Record<string, string>))
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
    window.setTimeout(() => titleField.current?.focus({ preventScroll: true }), 0)
  }

  const closeEditor = () => {
    setEditing(null)
    setDraft(null)
    setEditorSaid("")
    setArmed(false)
    closeButton.current?.focus({ preventScroll: true })
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
      void write(() => updateSnippet(id, patch), { thenClose: true })
      return
    }
    if (!may.create) return
    const body = snippetCreateBody(draft)
    if (!body) return
    void write(() => createSnippet(body), { thenClose: true })
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
    loading,
    error,
    menuFor,
  })

  return (
    <>
      <div
        className="sheet snippets-sheet"
        id="snippets-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="snippets-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="snippets-head">
          <div className="snippets-heading">
            <h2 id="snippets-title">{T.webSnippets}</h2>
            <span className="snippets-where" id="snippets-where">
              {model.project ? model.project.label : ""}
            </span>
          </div>
          {/* The only writing control outside the list, so the only one this
              file hides by hand; everything else is drawn, or not drawn, by
              `snippetsListHTML`. */}
          <button
            className="snippets-new"
            id="snippets-new"
            type="button"
            ref={newButton}
            hidden={!may.create || loading || !!error}
            aria-label={T.webSnippetNew}
            title={T.webSnippetNew}
            onClick={() => openEditor(null)}
          >
            ＋
          </button>
        </div>
        <div
          className="snippet-list"
          id="snippet-list"
          ref={list}
          onClick={pressed}
          dangerouslySetInnerHTML={{ __html: listHTML }}
        />
        <p className="said" id="snippets-said" role="status" aria-live="polite">
          {said}
        </p>
        <div className="buttons">
          <button className="chip" id="snippets-close" type="button" ref={closeButton} onClick={onClose}>
            {T.webClose}
          </button>
        </div>
      </div>
      {draft ? (
        <div className="overlay" id="snippet-editor" onClick={closeEditor}>
          <div
            className="sheet snippet-editor-sheet"
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
                type="button"
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
                {armed ? T.webSnippetDeleteAsk : T.webSnippetDelete}
              </button>
              <button className="chip" id="snippet-editor-cancel" type="button" onClick={closeEditor}>
                {T.webCancel}
              </button>
              <button className="chip confirm-go" id="snippet-editor-save" type="button" onClick={save}>
                {T.webSnippetSave}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  )
}
