import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import type { AssistantSkill, DevStacksReply, SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import { useBarFleet } from "./fleet.js"
import * as L from "../legacy/bridge.js"
import { askFocus } from "../legacy/screen-bridge.js"
import { words, agentsSaid, placeholderFor, shellsSaid } from "./words.js"
import {
  BAR_HIDDEN_EVENT,
  BAR_SHOWN_EVENT,
  BAR_STATE_EVENT,
  hideBar,
  inShell,
  reportHeight,
  type BarShellState,
} from "./shell.js"
import { historyBack, remember } from "./history.js"
import {
  clampSkillPickerIndex,
  filterSkills,
  heldSkills,
  loadSkills,
  selectedSkill,
  skillPrefix,
  skillQuery,
} from "../legacy/skills-bridge.js"
import { nextWord } from "../next-strings.js"
import { STACKS_REFRESH_MS, readStacks, stackLinks, stackStateSaid, stackTip } from "./stacks.js"

/**
 * The input bar: the Swift app's quick panel, drawn as a page.
 *
 * The original is `Sources/Controller.swift` (4,134 lines) with
 * `Sources/Panel.swift` (1,292) under it, and it is a native window because in
 * 2025 that was the only way to have one. It is a page here because the user
 * asked for a product that runs on three platforms: a card, a list and a hint
 * row are the same three things on all of them, and what is actually different
 * — a borderless window that a key summons from inside another application —
 * is what the shell keeps. `docs/shell-bridge.md` is the line between the two,
 * `docs/cross-platform.md` is what the other two platforms cannot do.
 *
 * Everything on screen here is a line of `Controller.swift` or `Panel.swift`,
 * named where it is drawn, and every word is a property of
 * `Copy+Chinese.swift` (see `words.ts`). Nothing asks the shell for data: the
 * rows are `/v1/sessions` over the same stream the console reads, sending is
 * `POST /v1/sessions/<id>/send`, and following the selection is
 * `POST /v1/sessions/<id>/focus` — the daemon's own routes, so the bar knows
 * exactly what the console knows and no fact has two sources.
 *
 * ## Two states that differ from `show()`, deliberately
 *
 * `Controller.show()` opens with `listMode = .none` and `keysShown = false`:
 * an input line, a hint row saying `⌘/ 快速鍵`, and nothing else. This bar
 * opens with the list up and the keys spelled out, which is the screen the
 * user gave as the specification for it, and is what `⌘K` and `⌘/` produce
 * there. The reason to diverge rather than replicate: the Swift panel's closed
 * state still has somewhere to go — `⌘J` opens the output pane over it — and
 * this one has no pane (see the hint row below), so a closed list would leave
 * a card with one line in it. Both keys still toggle, and these are the two
 * constants at the top of this file.
 */

/** `Controller.show()` leaves the list closed; see the note above. */
const LIST_OPEN_ON_SUMMON = true

/**
 * `Controller.ListMode`, less `mascots`, which this bar does not draw: which
 * list is under the box. One at a time, as there — ⌘K and ⌘S each toggle their
 * own and replace the other, and `/` puts the skills in their place.
 */
type ListMode = "none" | "sessions" | "stacks" | "skills"
const SUMMONED_LIST: ListMode = LIST_OPEN_ON_SUMMON ? "sessions" : "none"
/** `Controller.keysShown` starts false; see the note above. */
const KEYS_SHOWN_ON_SUMMON = true

/** `TargetRow`: the list draws at most nine rows, because ⌘1–⌘9 is nine keys. */
const MAX_ROWS = 9

/** `setHint`: a hint replaces the footer for a second and a half, then the footer comes back. */
const HINT_SECONDS = 1.5

/**
 * `hintsAll` (`Controller.buildPanel`), in its order.
 *
 * All nine, including `⇥ 換分頁`, which the screen the user gave does not show:
 * `KeyHintsView` is right-aligned and clipped to `W - padH * 2 - 120`, so on a
 * 720pt card the leftmost pair falls off the end. The stylesheet clips the same
 * way, from the same side, so the same eight survive — and a wider card shows
 * the ninth, as the original does.
 */
const HINTS: { key: string; label: string }[] = [
  { key: "⇥", label: words.hintSwitch },
  { key: "⌘K", label: words.hintList },
  { key: "⌘M", label: words.hintMascot },
  { key: "⌘J", label: words.hintOutput },
  { key: "⌘S", label: words.hintStacks },
  { key: "⌘F", label: words.hintFullscreen },
  { key: "⌘L", label: words.hintVoice },
  { key: "⌘R", label: words.hintOrder },
  { key: "⌘+", label: words.hintTextSize },
]

/** `applyHints()` with `keysShown` false: one pair, and it is the way back to the rest. */
const HINTS_CLOSED: { key: string; label: string }[] = [{ key: "⌘/", label: words.hintKeys }]

/** `MicButton`'s glyph: SF Symbols `mic` / `mic.fill`, as a path, at 13pt. */
function MicGlyph() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path
        fill="currentColor"
        d="M12 14.5a3 3 0 0 0 3-3v-6a3 3 0 1 0-6 0v6a3 3 0 0 0 3 3zm5.5-3a.75.75 0 0 1 1.5 0 6.5 6.5 0 0 1-5.75 6.455V21a.75.75 0 0 1-1.5 0v-3.045A6.5 6.5 0 0 1 5 11.5a.75.75 0 0 1 1.5 0 5 5 0 0 0 10 0z"
      />
    </svg>
  )
}

/** The project's mark, as `TargetRow.icon` draws it: `ProjectIcon.image(height: 11)`. */
function Mark({ icon, cellPx, className }: { icon: SessionRow["icon"]; cellPx: number; className: string }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => {
    L.paintIcon(ref.current, icon, cellPx)
  }, [icon, cellPx])
  return <canvas className={className} ref={ref} width={0} height={0} />
}

/**
 * `sessionRowDetail`: what a row says after its label.
 *
 * The branches and their order are the original's, because the order is the
 * precedence. Two of its inputs have no field on this wire and are therefore
 * absent rather than guessed: the number of background agents
 * (`runningAgents`, which reads the Swift app's subagent files) and the
 * coordination wait (`coordinationWaitSaid`, which reads its orchestrator
 * store). Both are named in `docs/cross-platform.md`; a row that cannot say
 * them says nothing, which is what the original does when it has nothing.
 */
function detailFor(row: SessionRow): { html: string; busy: boolean } | null {
  const shells = row.shells?.length ?? 0
  const away = shells
  const tail = shells > 0 ? "  ·  " + shellsSaid(shells) : ""
  if (row.state === "working") {
    // `room`: shorter when something has to go after it, because "one shell is
    // running" is worth more than the last eleven characters of a sentence that
    // is already ellipsised.
    const room = away > 0 ? 28 : 44
    const line = row.line ?? ""
    const cut = line.length > room ? line.slice(0, room - 1) + "…" : line
    return { html: L.escapeHTML(cut) + L.escapeHTML(tail), busy: true }
  }
  if (row.state === "waiting") {
    return {
      html: '<span class="waiting">● ' + L.escapeHTML(words.sessionWaiting) + "</span>" + L.escapeHTML(tail),
      busy: false,
    }
  }
  // idle and unknown: "not nil any more, if anything is out".
  if (away === 0) return null
  return { html: L.escapeHTML(shellsSaid(shells)), busy: false }
}

/** `agentsSaid` has no input on this wire; kept imported so the omission is visible, not silent. */
void agentsSaid

/**
 * `rebuildRows()` for `.stacks`: at most nine rows, one per project, and a last
 * line that says what this list does not do. Not buttons: on the Swift row
 * "the buttons are the only things on the row that act; links open; the rest
 * is just text to read", and this row has no buttons (see `stacks.ts`).
 *
 * A port or an address is a link in a browser. In the Mac shell's bar window
 * it is text: that window opens no new windows, so a link there would be a
 * thing that does nothing when pressed.
 */
function StackList({
  reading,
  links,
}: {
  reading: { reply: DevStacksReply | null; failed: boolean }
  links: boolean
}) {
  const list = reading.reply?.stacks ?? []
  const place = (label: string, href: string) =>
    links ? (
      <a className="stack-place" href={href} target="_blank" rel="noopener noreferrer" title={href}>
        {label}
      </a>
    ) : (
      <span className="stack-place" title={href}>
        {label}
      </span>
    )
  let note: string
  if (!reading.reply) note = reading.failed ? nextWord("stacksFailed") : words.scanning
  else if (!list.length) note = nextWord("stacksNone")
  else note = nextWord("stacksCommandsNotRun")
  return (
    <div className="bar-list bar-stacks" role="list">
      {list.slice(0, MAX_ROWS).map((stack, at) => {
        const state = stackStateSaid(stack)
        const { ports, hosts } = stackLinks(stack)
        return (
          <div className="bar-row bar-stack" role="listitem" key={stack.root} title={stackTip(stack)}>
            <span className="bar-badge">⌘{at + 1}</span>
            <span className="bar-label stack-name" style={{ color: stack.icon?.accent }}>
              {stack.name}
            </span>
            <span className="bar-detail">
              <span className="stack-state" data-tone={state.tone}>
                {state.text}
              </span>
              {ports.map((p) => (
                <span className="stack-port" key={"p" + p.label}>
                  {place(p.label, p.href)}
                </span>
              ))}
              {hosts.map((h) => (
                <span className="stack-host" key={"h" + h.href}>
                  ↗ {place(h.label, h.href)}
                </span>
              ))}
            </span>
          </div>
        )
      })}
      <p className="bar-note" data-warn={reading.failed ? "on" : "off"}>
        {note}
        {reading.reply?.truncated ? " " + nextWord("stacksTruncated") : ""}
        {reading.reply && list.length > MAX_ROWS ? ` (${MAX_ROWS}/${list.length})` : ""}
      </p>
    </div>
  )
}

export default function Bar() {
  const fleet = useBarFleet()
  const rows = useMemo(() => (fleet.snapshot?.sessions ?? []).slice(0, MAX_ROWS), [fleet.snapshot])

  const [listMode, setListMode] = useState<ListMode>(SUMMONED_LIST)
  const listOpen = listMode === "sessions"
  // `skillMatches` and `skillIndex`: the menu's rows, and the highlighted one.
  const [skillMatches, setSkillMatches] = useState<AssistantSkill[]>([])
  const [skillIndex, setSkillIndex] = useState(0)
  // Bumped when a catalog arrives, so the menu is drawn for the box as it is then.
  const [skillsArrived, setSkillsArrived] = useState(0)
  // `stackRows` and `stackCache`, as one answer: the list and its states.
  const [stacks, setStacks] = useState<{ reply: DevStacksReply | null; failed: boolean }>({
    reply: null,
    failed: false,
  })
  const [keysShown, setKeysShown] = useState(KEYS_SHOWN_ON_SUMMON)
  const [text, setText] = useState("")
  // `stickyID` (`Controller`): the selection is an id, not an index, so a list
  // that arrives with one more row in it does not move the highlight under the
  // hand. `Controller` keeps both and reconciles; this keeps the id and derives
  // the index, which is the same rule with one fewer thing to get wrong.
  const [stickyID, setStickyID] = useState<string | null>(null)
  const [hint, setHint] = useState<{ said: string; warn: boolean } | null>(null)
  const [shell, setShell] = useState<BarShellState>({ follow: false, hotkey: "" })

  const boxRef = useRef<HTMLTextAreaElement>(null)
  const cardRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const hintTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const index = useMemo(() => {
    const at = stickyID ? rows.findIndex((r) => r.id === stickyID) : -1
    return at < 0 ? (rows.length ? 0 : -1) : at
  }, [rows, stickyID])
  const current: SessionRow | null = index >= 0 ? (rows[index] ?? null) : null

  const textRef = useRef(text)
  textRef.current = text
  const rowsRef = useRef(rows)
  rowsRef.current = rows
  const indexRef = useRef(index)
  indexRef.current = index
  const listModeRef = useRef(listMode)
  listModeRef.current = listMode
  const skillsRef = useRef({ matches: skillMatches, index: skillIndex })
  skillsRef.current = { matches: skillMatches, index: skillIndex }
  const stacksRef = useRef(stacks)
  stacksRef.current = stacks
  const shellRef = useRef(shell)
  shellRef.current = shell
  const currentRef = useRef(current)
  currentRef.current = current

  /**
   * `updateSkillSuggestions()`: the box as it is now decides whether the
   * skills are the list. A catalog not yet held is asked for once per session,
   * with the list already switched to it, as the Swift panel does; a catalog
   * that arrives for a session no longer selected is kept and not drawn.
   */
  const skillTarget = current?.id ?? null
  const skillAssistant = current?.assistant
  useEffect(() => {
    const q = skillTarget ? skillQuery(text, skillAssistant) : null
    if (q === null || !skillTarget) {
      setListMode((was) => (was === "skills" ? "none" : was))
      setSkillMatches((was) => (was.length ? [] : was))
      return
    }
    const held = heldSkills(skillTarget)
    setListMode("skills")
    if (!held) {
      setSkillMatches([])
      setSkillIndex(0)
      let live = true
      void loadSkills(skillTarget).then(() => {
        if (live) setSkillsArrived((n) => n + 1)
      })
      return () => {
        live = false
      }
    }
    const matches = filterSkills(held, q)
    setSkillMatches(matches)
    setSkillIndex((at) => clampSkillPickerIndex(at, matches.length))
  }, [text, skillTarget, skillAssistant, skillsArrived])

  /** `refreshStacks()`: read when the list opens, and again while it stays open. */
  const stacksOpen = listMode === "stacks"
  useEffect(() => {
    if (!stacksOpen) return
    let live = true
    const read = () => {
      readStacks()
        .then((reply) => {
          if (live) setStacks({ reply, failed: false })
        })
        .catch(() => {
          if (live) setStacks((was) => ({ reply: was.reply, failed: true }))
        })
    }
    read()
    const timer = setInterval(read, STACKS_REFRESH_MS)
    return () => {
      live = false
      clearInterval(timer)
    }
  }, [stacksOpen])

  /** `setHint(_:warn:)`: the footer says one thing for a moment and then goes back to itself. */
  const say = useCallback((said: string, warn: boolean) => {
    clearTimeout(hintTimer.current)
    setHint({ said, warn })
    hintTimer.current = setTimeout(() => setHint(null), HINT_SECONDS * 1000)
  }, [])

  /**
   * `follow(_:)`: move the terminal's own tab to the session the bar now points
   * at, off the main path and unwaited-for — "a courtesy rather than part of
   * the switch".
   *
   * Only when the shell says so, and no shell says so yet: see
   * `BarShellState.follow` and `docs/cross-platform.md`. In the Swift app this
   * reveal does **not** activate the terminal, because activating it would take
   * the keyboard out of the box somebody is typing in; this daemon's `/focus`
   * has no way to ask for that yet, so the wire is in place and switched off
   * rather than switched on and wrong.
   */
  const follow = useCallback((row: SessionRow) => {
    if (!shellRef.current.follow) return
    void askFocus(row.id).catch(() => {
      /* a courtesy that failed is not news; the switch itself has happened */
    })
  }, [])

  /** `pick(_:closeList:)`: the highlight moves, and the terminal follows it. */
  const pick = useCallback(
    (at: number, closeList = true) => {
      const list = rowsRef.current
      if (at < 0 || at >= list.length) return
      setStickyID(list[at].id)
      if (closeList) setListMode("none")
      follow(list[at])
    },
    [follow],
  )

  /** `cycle(forward:)`: ⇥ and ⇧⇥ walk the list and wrap, without closing it. */
  const cycle = useCallback(
    (forward: boolean) => {
      const list = rowsRef.current
      if (list.length < 2) return
      const at = indexRef.current < 0 ? 0 : indexRef.current
      pick((at + (forward ? 1 : list.length - 1)) % list.length, false)
    },
    [pick],
  )

  /** `showList(_:)`: the same key opens its list and closes it; another key's list replaces it. */
  const showList = useCallback((mode: ListMode) => {
    setListMode((was) => (was === mode ? "none" : mode))
  }, [])

  /**
   * `acceptSkill()`: complete, do not execute. The box becomes the invocation
   * and a space; the next Return sends the finished line through the same path
   * as every other prompt, because many skills take arguments.
   */
  const acceptSkill = useCallback((at?: number): boolean => {
    if (listModeRef.current !== "skills") return false
    const { matches, index } = skillsRef.current
    const skill = selectedSkill(matches, at ?? index)
    if (!skill) return false
    const said = skillPrefix(currentRef.current?.assistant) + skill.name + " "
    setText(said)
    setListMode("none")
    setSkillMatches([])
    requestAnimationFrame(() => {
      const box = boxRef.current
      if (box) box.setSelectionRange(said.length, said.length)
    })
    return true
  }, [])

  /**
   * `choose(_:)`: ⌘n is "the nth row of whatever is open". Over the server
   * list the Swift app started or restarted that stack; this daemon runs none
   * of a project's commands, so the key says so rather than doing nothing.
   */
  const choose = useCallback(
    (at: number) => {
      const mode = listModeRef.current
      if (mode === "skills") {
        acceptSkill(at)
        return
      }
      if (mode === "stacks") {
        const stack = stacksRef.current.reply?.stacks[at]
        if (stack) say(nextWord("stackNoAction", { name: stack.name }), true)
        return
      }
      pick(at)
    },
    [acceptSkill, pick, say],
  )

  /** `submit()`: send what is in the box to the session the bar points at, then go away. */
  const submit = useCallback(() => {
    const body = textRef.current.trim()
    if (!body) {
      hideBar()
      return
    }
    const list = rowsRef.current
    const at = indexRef.current
    const target = at >= 0 ? list[at] : null
    if (!target) {
      say(words.nothingToSend, true)
      return
    }
    remember(body)
    setText("")
    void client
      .send(target.id, body)
      .then(() => {
        // "Let the jump finish before closing": the original waits 0.18s so
        // that pressing Enter has a result on screen. Here the wait is the
        // send's own answer, which is the same beat and is also the truth —
        // a bar that vanished before the send failed would have swallowed it.
        hideBar()
      })
      .catch((err: unknown) => {
        // `restoreAfterFailure`: "Someone typed two hundred characters; an
        // iTerm hiccup must not swallow them."
        setText(body)
        boxRef.current?.focus()
        // `setHint(error, warn: true)` with the error the send gave. A refusal
        // carries a sentence written for a person (`RefusalError.detail`); a
        // transport failure has only its own words, and `sendFailed` — the
        // original's "送不出去" — is the honest thing to put on a hint row that
        // is one line wide.
        say(err instanceof RefusalError && err.detail ? err.detail : words.sendFailed, true)
      })
  }, [say])

  /** `PromptTextView`: the box grows with the text and stops at `maxTextHeight`. */
  useLayoutEffect(() => {
    const box = boxRef.current
    if (!box) return
    box.style.height = "auto"
    box.style.height = box.scrollHeight + "px"
  }, [text])

  /**
   * The card's height is the window's height; see `shell.ts` for why the page
   * owns it.
   *
   * Measured after **every** render, with no dependency list, and separately
   * watched for the changes a render does not cause — a font arriving, the
   * window being made wider. Both, because neither alone is enough: a
   * `ResizeObserver` callback is delivered on a rendering step, and a webview
   * whose window has been ordered out may not have one for minutes, so a card
   * that grew while it was away would come back with the window still at its
   * old size. Measured here: in a tab Chrome had stopped painting, no observer
   * callback and no animation frame arrived at all, while layout effects kept
   * running and `getBoundingClientRect` kept answering.
   *
   * It costs one `getBoundingClientRect` per render, and `reportHeight` drops
   * the ones that did not change, so nothing crosses to the shell for a render
   * that moved nothing.
   */
  useLayoutEffect(() => {
    const card = cardRef.current
    if (card) reportHeight(card.getBoundingClientRect().height)
  })
  useLayoutEffect(() => {
    const card = cardRef.current
    if (!card) return
    const observer = new ResizeObserver(() => reportHeight(card.getBoundingClientRect().height))
    observer.observe(card)
    return () => observer.disconnect()
  }, [])

  /** The working rows' spinners, on the one clock the console already keeps. */
  useLayoutEffect(() => {
    const canvases = Array.from(listRef.current?.querySelectorAll<HTMLCanvasElement>("canvas.spin") ?? [])
    for (const canvas of canvases) L.paintSpinner(canvas)
    L.registerSpinners(canvases)
  }, [rows, listMode])

  /**
   * What the shell says, and what a summon means here.
   *
   * `shown` is `Controller.show()` minus the parts that are the window's: the
   * list and the keys go back to what a fresh summon has, the history cursor
   * is let go, and the keyboard lands in the box. The text is **kept**, as it
   * is there — a summon is not a way to lose a half-written message.
   */
  useEffect(() => {
    const onState = (ev: Event) => {
      const next = (ev as CustomEvent<BarShellState>).detail
      if (next) setShell(next)
    }
    const onShown = () => {
      setListMode(SUMMONED_LIST)
      setKeysShown(KEYS_SHOWN_ON_SUMMON)
      setHint(null)
      historyBack.reset()
      // `panel.makeFirstResponder(textView)`, after the frame the window is
      // shown in: a box that is not on screen yet cannot take the caret.
      requestAnimationFrame(() => boxRef.current?.focus())
    }
    const onHidden = () => {
      setHint(null)
    }
    window.addEventListener(BAR_STATE_EVENT, onState)
    window.addEventListener(BAR_SHOWN_EVENT, onShown)
    window.addEventListener(BAR_HIDDEN_EVENT, onHidden)
    return () => {
      window.removeEventListener(BAR_STATE_EVENT, onState)
      window.removeEventListener(BAR_SHOWN_EVENT, onShown)
      window.removeEventListener(BAR_HIDDEN_EVENT, onHidden)
    }
  }, [])

  /** In a browser there is no summon; the box still takes the caret on arrival. */
  useEffect(() => {
    boxRef.current?.focus()
  }, [])

  /**
   * `wireKeys()` and `PromptTextView.keyDown`, in the original's order: one
   * press does one thing, and a `return` ends this listener and nothing else.
   *
   * It is on the document rather than on the box because the shell hands every
   * key to the page (`docs/shell-bridge.md`): there is no menu bar in this
   * window to catch ⌘K first, and nothing else in the card takes a key.
   */
  const onKey = useCallback(
    (ev: KeyboardEvent) => {
      const meta = ev.metaKey || ev.ctrlKey
      const key = ev.key

      if (key === "Escape") {
        ev.preventDefault()
        // `onCancel`: the list closes first, and only a bar with nothing open
        // goes away. Esc is how you say "this one is done".
        if (listModeRef.current !== "none") setListMode("none")
        else hideBar()
        return
      }

      if (key === "Tab") {
        ev.preventDefault()
        // "Tab or Return completes an open suggestion before they keep their ordinary meanings."
        if (acceptSkill()) return
        cycle(!ev.shiftKey)
        return
      }

      if (meta && key >= "1" && key <= "9") {
        ev.preventDefault()
        choose(Number(key) - 1)
        return
      }

      if (meta && (key === "k" || key === "K")) {
        ev.preventDefault()
        showList("sessions")
        return
      }

      if (meta && (key === "s" || key === "S")) {
        ev.preventDefault()
        showList("stacks")
        return
      }

      if (meta && key === "/") {
        ev.preventDefault()
        setKeysShown((on) => !on)
        return
      }

      if (key === "ArrowDown" || key === "ArrowUp") {
        const delta = key === "ArrowDown" ? 1 : -1
        // `handleArrow`: with the list open the arrows move the selection; with
        // it closed they walk back through what has been sent.
        if (listModeRef.current === "skills") {
          ev.preventDefault()
          const count = skillsRef.current.matches.length
          if (count) setSkillIndex((at) => Math.max(0, Math.min(count - 1, at + delta)))
          return
        }
        if (listModeRef.current === "sessions") {
          ev.preventDefault()
          const at = indexRef.current < 0 ? 0 : indexRef.current
          pick(Math.max(0, Math.min(rowsRef.current.length - 1, at + delta)), false)
          return
        }
        const said = delta < 0 ? historyBack.older(textRef.current) : historyBack.newer()
        if (said === null) return
        ev.preventDefault()
        setText(said)
        requestAnimationFrame(() => {
          const box = boxRef.current
          if (box) box.setSelectionRange(box.value.length, box.value.length)
        })
        return
      }

      if (key === "Enter" && !ev.shiftKey && !meta) {
        // Not while an input method is mid-word: Return there takes the candidate.
        if (ev.isComposing || ev.keyCode === 229) return
        ev.preventDefault()
        if (acceptSkill()) return
        submit()
        return
      }
    },
    [acceptSkill, choose, cycle, pick, showList, submit],
  )
  const keyRef = useRef(onKey)
  keyRef.current = onKey
  useEffect(() => {
    const listener = (ev: KeyboardEvent) => keyRef.current(ev)
    document.addEventListener("keydown", listener)
    return () => document.removeEventListener("keydown", listener)
  }, [])

  const mark = current ? L.markForSession(current) : null
  const project = L.projectLabel(current?.cwd)
  const hints = keysShown ? HINTS : HINTS_CLOSED

  return (
    <div className="bar-card" ref={cardRef}>
      {/* `chevron`, the box, and `micButton`. A form so that Enter means send
          even where a key listener does not run, and `onSubmit` is the one
          place a send starts. */}
      <form
        className="bar-input"
        onSubmit={(ev) => {
          ev.preventDefault()
          submit()
        }}
      >
        <span className="bar-chevron" aria-hidden="true">
          ❯
        </span>
        <textarea
          className="bar-text"
          ref={boxRef}
          rows={1}
          value={text}
          placeholder={placeholderFor(current?.assistant)}
          aria-label={placeholderFor(current?.assistant)}
          spellCheck={false}
          onChange={(ev) => setText(ev.target.value)}
        />
        {/* `MicButton`. Drawn, and inert: dictation in this window is
            `POST /v1/voice` plus the recorder the composer already has, and
            neither is wired to this card yet (docs/cross-platform.md). The
            title is the original's, so the button says what it is for. */}
        <button className="bar-mic" type="button" title={words.hintVoice} aria-label={words.hintVoice} disabled>
          <MicGlyph />
        </button>
      </form>

      {listOpen && rows.length > 0 && (
        <div className="bar-list" role="listbox" ref={listRef}>
          {rows.map((row, at) => {
            const detail = detailFor(row)
            const selected = at === index
            return (
              <button
                key={row.id}
                className="bar-row"
                type="button"
                role="option"
                aria-selected={selected}
                onClick={() => pick(at)}
              >
                <span className="bar-badge">⌘{at + 1}</span>
                <Mark icon={L.markForSession(row) ?? undefined} cellPx={2.75} className="bar-mark" />
                <span className="bar-label">{row.label ?? row.id}</span>
                {L.hasLogo(row.assistant) && (
                  <span
                    className="bar-who"
                    dangerouslySetInnerHTML={{
                      __html:
                        L.assistantLogoHTML(row.assistant) +
                        "<span>" +
                        L.escapeHTML(L.assistantDisplayName(row.assistant)) +
                        "</span>",
                    }}
                  />
                )}
                {detail && (
                  <span
                    className="bar-detail"
                    dangerouslySetInnerHTML={{
                      __html: (detail.busy ? '<canvas class="spin"></canvas>' : "") + detail.html,
                    }}
                  />
                )}
              </button>
            )
          })}
        </div>
      )}

      {listMode === "skills" && skillMatches.length > 0 && (
        // `rebuildRows()` for `.skills`: the command as it will be typed, and
        // the one line of description after it.
        <div className="bar-list" role="listbox" aria-label={`${current?.assistant === "codex" ? "Codex" : "Claude Code"} skills`}>
          {skillMatches.map((skill, at) => (
            <button
              key={skill.name}
              className="bar-row bar-skill"
              type="button"
              role="option"
              aria-selected={at === skillIndex}
              onMouseDown={(ev) => ev.preventDefault()}
              onClick={() => acceptSkill(at)}
            >
              <span className="bar-badge">⌘{at + 1}</span>
              <span className="bar-label">{skillPrefix(current?.assistant) + skill.name}</span>
              {skill.description && (
                <span className="bar-detail" title={skill.description}>
                  {skill.description}
                </span>
              )}
            </button>
          ))}
        </div>
      )}

      {listMode === "stacks" && <StackList reading={stacks} links={!inShell()} />}

      <div className="bar-hints">
        {/* `updateTargetLabel`, bottom left. A hint takes the whole footer for
            a second and a half and then it comes back (`setHint`). */}
        {hint ? (
          <div className="bar-target">
            <span className="said" data-warn={hint.warn ? "on" : "off"}>
              {hint.said}
            </span>
          </div>
        ) : (
          <div className="bar-target" data-assistant={current?.assistant ? "on" : "off"}>
            {current ? (
              <>
                <span className="dot" aria-hidden="true">
                  ●
                </span>
                {mark && <Mark icon={mark} cellPx={2.75} className="bar-mark" />}
                {project && (
                  <span className="project" style={{ color: mark?.accent }}>
                    {project}
                  </span>
                )}
                <span className="session">{current.label ?? current.id}</span>
                {rows.length > 1 && (
                  <span className="index">
                    {index + 1}/{rows.length}
                  </span>
                )}
              </>
            ) : (
              <span className="nothing">{fleet.loaded ? words.noSession : words.scanning}</span>
            )}
          </div>
        )}

        {/* `KeyHintsView`, right-aligned, and clicking it is the other way to
            open the full set — "for people who do not know the key yet, which
            is everyone, the first time". */}
        <div
          className="bar-keys"
          onClick={() => setKeysShown((on) => !on)}
          title={inShell() && shell.hotkey ? shell.hotkey : undefined}
        >
          {/* Handed over backwards, because the row is laid out from the right
              — see `.bar-keys` in bar.css. `HINTS` itself stays in the
              original's order, which is the order it is read in. */}
          {[...hints].reverse().map((h) => (
            <span className="pair" key={h.key}>
              <span className="cap">{h.key}</span>
              {h.label && <span className="said">{h.label}</span>}
            </span>
          ))}
        </div>
      </div>
    </div>
  )
}
