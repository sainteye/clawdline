/*
 * Where a Session shows its role using a Skill: the role bot in the detail
 * header (a toggle for the Skill strip, and a hop on `applied`), the strip of
 * this Session's own Skills, a card in the transcript per Skill, a summary
 * when the Session's task has finished, and a small icon beside the bot on a
 * list row. Data: skill-feed.ts; rules: skill-use.ts.
 */
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type ReactElement } from "react"
import { createPortal } from "react-dom"
import type { Icon, Persona, SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { PersonaBot } from "./PersonaBot.js"
import { arrivedLive, useSessionSnapshot, useSkillLedger, useSkillMoments, useSkillMotion } from "./skill-feed.js"
import { MomentQueue, SaidLine, SkillListFocus, receiptBelongs, skillSummary, type SessionSkillSnapshot, type SkillMoment, type SkillUse, type SkillUseStatus, type SnapshotSkill } from "./skill-use.js"
import "./skill-use.css"

/** One Skill's own pixel icon, drawn once at whole pixels per cell. */
export function SkillIcon({ icon, cellPx = 2, className = "skill-icon" }: { icon?: Icon; cellPx?: number; className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => { L.paintIcon(ref.current, icon, cellPx) }, [icon, cellPx])
  return <canvas className={className} ref={ref} width={0} height={0} aria-hidden="true" />
}

export function skillUseLine(status: SkillUseStatus, name: string): string {
  return nextWord(status === "read" ? "skillUseReading" : status === "applied" ? "skillUseApplied" : "skillUseFailed", { skill: name })
}

function statusWord(status: SkillUseStatus | undefined): string {
  return nextWord(status === "read" ? "skillStatusRead" : status === "applied" ? "skillStatusApplied" : status === "failed" ? "skillStatusFailed" : "skillStatusUnused")
}

/**
 * The small mark beside a state's words: dots that pulse while a role
 * consults a Skill (still dots when nothing may move), a green check once
 * applied, the muted slash for a failed use, a muted dash when unused. Always
 * hidden from a screen reader; the words beside it are the label.
 */
export function SkillStatusMark({ status }: { status: SkillUseStatus | undefined }) {
  if (status === "read") return <span className="skill-mark skill-mark-read" aria-hidden="true"><i /><i /><i /></span>
  if (status === "applied") return <svg className="skill-mark skill-mark-applied" viewBox="0 0 12 12" width="12" height="12" aria-hidden="true">
    <path d="M2.5 6.4 5 8.8 9.6 3.4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
  if (status === "failed") return <span className="skill-mark skill-mark-failed" aria-hidden="true"><span className="skill-chip-failed" /></span>
  return <span className="skill-mark skill-mark-unused" aria-hidden="true" />
}

export interface SessionSkills {
  snapshot: SessionSkillSnapshot | null
  uses: Map<string, SkillUse>
  motion: boolean
}

/**
 * This Session's Skills: its launch snapshot (only for a Session launched with
 * a role) and what the shared feed's receipts say about each.
 */
export function useSessionSkills(row: Pick<SessionRow, "persona" | "sessionId"> | null | undefined, read = true): SessionSkills {
  const conversation = read && row?.persona ? row.sessionId : undefined
  const snapshot = useSessionSnapshot(conversation)
  const ledger = useSkillLedger(snapshot?.skills.length ? snapshot.conversationId : null, snapshot?.machine ?? null)
  const motion = useSkillMotion(snapshot?.scopeId, snapshot?.machine ?? null)
  const uses = ledger ? ledger.usesFor(snapshot) : new Map<string, SkillUse>()
  return { snapshot, uses, motion }
}

/** A live moment this Session's snapshot owns, held while it plays. */
function usePlaying(snapshot: SessionSkillSnapshot | null, hold?: number): SkillMoment | null {
  const [playing, setPlaying] = useState<SkillMoment | null>(null)
  const queue = useMemo(() => new MomentQueue<SkillMoment>(setPlaying, hold), [hold])
  useEffect(() => () => queue.stop(), [queue])
  useEffect(() => { queue.stop(); setPlaying(null) }, [queue, snapshot?.conversationId, snapshot?.snapshotId])
  const listener = useCallback((moment: SkillMoment) => { if (receiptBelongs(moment.receipt, snapshot)) queue.push(moment) }, [queue, snapshot])
  useSkillMoments(!!snapshot?.skills.length, listener, snapshot?.machine ?? null)
  return playing
}

/** Whether a Session's strip is open: opened by its first live receipt, or by the toggle. */
const stripChoice = new Map<string, boolean>()

/** A live use's words, shown for a few seconds (`SaidLine`). */
export interface SkillSaid { skillId: string; status: SkillUseStatus; seq: number; leaving: boolean }

/** The live uses this Session's snapshot owns, each said for a few seconds and then not. */
function useSaid(snapshot: SessionSkillSnapshot | null): SkillSaid | null {
  const [said, setSaid] = useState<SkillSaid | null>(null)
  const line = useMemo(() => new SaidLine<SkillMoment>((shown) => setSaid(shown ? { skillId: shown.value.receipt.skillId,
    status: shown.value.receipt.status, seq: shown.value.receipt.seq, leaving: shown.leaving } : null)), [])
  useEffect(() => () => line.stop(), [line])
  useEffect(() => { line.stop(); setSaid(null) }, [line, snapshot?.conversationId, snapshot?.snapshotId])
  const listener = useCallback((moment: SkillMoment) => { if (receiptBelongs(moment.receipt, snapshot)) line.push(moment) }, [line, snapshot])
  useSkillMoments(!!snapshot?.skills.length, listener, snapshot?.machine ?? null)
  return said
}

export interface DetailSkills extends SessionSkills {
  playing: SkillMoment | null
  said: SkillSaid | null
  open: boolean
  entering: boolean
  toggle: () => void
}

export function useDetailSkills(row: SessionRow | null, read: boolean): DetailSkills {
  const skills = useSessionSkills(row, read)
  const { snapshot, uses } = skills
  const playing = usePlaying(snapshot)
  const said = useSaid(snapshot)
  const key = snapshot ? snapshot.conversationId : ""
  const [, redraw] = useState(0)
  const [entering, setEntering] = useState(false)
  // The first receipt this Session reports opens its strip and keeps it open;
  // a reload with receipts already there opens it without the entrance.
  useEffect(() => {
    if (!key || stripChoice.has(key) || !uses.size) return
    stripChoice.set(key, true)
    setEntering(!!playing)
    redraw((n) => n + 1)
  }, [key, uses.size, playing])
  useEffect(() => { if (!entering) return; const t = setTimeout(() => setEntering(false), 300); return () => clearTimeout(t) }, [entering])
  const toggle = useCallback(() => {
    if (!key) return
    stripChoice.set(key, !stripChoice.get(key))
    setEntering(false)
    redraw((n) => n + 1)
  }, [key])
  return { ...skills, playing, said, open: !!key && !!stripChoice.get(key), entering: entering && skills.motion, toggle }
}

/** The header's role bot, as the strip's toggle; it hops once on `applied`. */
export function SkillBotToggle({ persona, name, skills }: { persona: Persona; name: string; skills: DetailSkills }) {
  const hop = skills.motion && skills.playing?.receipt.status === "applied" ? skills.playing.receipt.seq : 0
  return (
    <button type="button" className="skill-bot-toggle" id="skill-bot-toggle" aria-expanded={skills.open}
      aria-controls="skill-strip" aria-label={nextWord("skillStripToggle", { role: name })} title={nextWord("skillStripToggle", { role: name })}
      onClick={skills.toggle}>
      <span className="skill-bot-hop" key={hop} data-hop={hop ? "on" : undefined}>
        <PersonaBot persona={persona} cellPx={2} className="skill-bot" />
      </span>
      <span className="header-menu-label">{nextWord("skillStripLabel")}</span>
    </button>
  )
}

/**
 * The strip of this Session's Skills: used ones lit, unused dimmed. Each chip
 * is a button that opens the list of this role's Skills on that Skill. A live
 * use is said in words for a few seconds; the lasting state is the chip's own.
 */
export function SkillStrip({ skills, role }: { skills: DetailSkills; role: string }) {
  const { snapshot, uses, playing, motion, said } = skills
  const [listOn, setListOn] = useState<string | null>(null)
  const list = useMemo(() => new SkillListFocus(setListOn), [])
  useEffect(() => () => list.close(), [list])
  const strip = useRef<HTMLDivElement>(null)
  const close = useCallback(() => list.close(), [list])
  const onListKey = useCallback((key: string) => list.key(key), [list])
  if (!snapshot?.skills.length || !skills.open) return null
  const saidSkill = said && snapshot.skills.find((skill) => skill.id === said.skillId)
  return (
    <div className="skill-strip" id="skill-strip" ref={strip} data-enter={skills.entering ? "on" : undefined}>
      <ul className="skill-strip-list" aria-label={nextWord("skillStripLabel")}>
        {snapshot.skills.map((skill) => {
          const status = uses.get(skill.id)?.status
          const play = motion && playing?.receipt.skillId === skill.id ? playing.receipt.status : undefined
          return (
            <li key={skill.id} className="skill-strip-item">
              <button type="button" className="skill-chip" data-state={status ?? "unused"} data-skill={skill.id} aria-haspopup="dialog"
                aria-expanded={listOn === skill.id} title={skill.name + " · " + statusWord(status)}
                onClick={(event) => list.open(skill.id, event.currentTarget)}>
                <span className="skill-chip-icon" key={play ? playing!.receipt.seq : 0} data-play={play}>
                  <SkillIcon icon={skill.icon} />
                </span>
                <span className="skill-chip-name">{skill.name}</span>
                {status === "failed" && <span className="skill-chip-failed" aria-hidden="true" />}
                <span className="sr-only skill-chip-status">{statusWord(status)}</span>
              </button>
            </li>
          )
        })}
      </ul>
      <p className="skill-strip-said" role="status" aria-live="polite" data-leaving={said?.leaving ? "on" : undefined} data-still={motion ? undefined : "on"}>
        {said && saidSkill ? <span className="skill-strip-said-words" key={said.seq}><SkillStatusMark status={said.status} />{skillUseLine(said.status, saidSkill.name)}</span> : null}
      </p>
      {listOn && <SkillList snapshot={snapshot} uses={uses} role={role} focus={listOn} motion={motion} anchor={strip.current} onKey={onListKey} onClose={close} />}
    </div>
  )
}

/**
 * The list of this role's Skills: a sheet on a phone, a panel under the strip
 * on a wider screen. Opened on one Skill, which takes the focus; Escape, the
 * close button or a press outside closes it, and the caller returns the focus
 * to the chip that opened it.
 */
export function SkillList({ snapshot, uses, role, focus, motion, anchor, onKey, onClose }: { snapshot: SessionSkillSnapshot; uses: ReadonlyMap<string, SkillUse>;
  role: string; focus: string; motion: boolean; anchor: HTMLElement | null; onKey: (key: string) => boolean; onClose: () => void }) {
  const panel = useRef<HTMLDivElement>(null)
  const [place, setPlace] = useState<{ top: number; left: number; width: number } | null>(null)
  useLayoutEffect(() => {
    const rect = anchor?.getBoundingClientRect()
    if (rect) setPlace({ top: Math.round(rect.bottom + 6), left: Math.round(rect.left + 12), width: Math.round(Math.min(420, rect.width - 24)) })
  }, [anchor])
  useLayoutEffect(() => {
    const row = [...(panel.current?.querySelectorAll<HTMLElement>(".skill-list-row") ?? [])].find((el) => el.dataset.skill === focus)
    row?.focus({ preventScroll: true })
    row?.scrollIntoView?.({ block: "nearest" })
  }, [focus])
  useEffect(() => {
    const listen = (event: KeyboardEvent) => { if (onKey(event.key)) { event.preventDefault(); event.stopPropagation() } }
    document.addEventListener("keydown", listen, true)
    return () => document.removeEventListener("keydown", listen, true)
  }, [onKey])
  // Tab stays inside the list while it is open.
  const trap = (event: ReactKeyboardEvent) => {
    if (event.key !== "Tab" || !panel.current) return
    const stops = [...panel.current.querySelectorAll<HTMLElement>("button, .skill-list-row")]
    if (!stops.length) return
    const at = stops.indexOf(document.activeElement as HTMLElement)
    const next = event.shiftKey ? (at <= 0 ? stops.length - 1 : at - 1) : (at < 0 || at === stops.length - 1 ? 0 : at + 1)
    event.preventDefault()
    stops[next].focus()
  }
  const style = place ? { "--skill-list-top": place.top + "px", "--skill-list-left": place.left + "px", "--skill-list-width": place.width + "px" } as CSSProperties : undefined
  return createPortal(
    <div className="skill-list-scrim" onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <div className="skill-list" ref={panel} role="dialog" aria-modal="true" aria-labelledby="skill-list-title" style={style}
        data-still={motion ? undefined : "on"} onKeyDown={trap}>
        <div className="skill-list-head">
          <h2 id="skill-list-title">{nextWord("skillListTitle", { role })}</h2>
          <button type="button" className="skill-list-close" aria-label={nextWord("skillListClose")} title={nextWord("skillListClose")} onClick={onClose}>
            <svg viewBox="0 0 12 12" width="12" height="12" aria-hidden="true"><path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
          </button>
        </div>
        <p className="skill-list-note">{nextWord("skillListNote")}</p>
        <ul className="skill-list-rows">
          {snapshot.skills.map((skill) => {
            const status = uses.get(skill.id)?.status
            return (
              <li key={skill.id} className="skill-list-row" data-skill={skill.id} data-state={status ?? "unused"} data-focus={skill.id === focus ? "on" : undefined} tabIndex={-1}>
                <span className="skill-list-icon"><SkillIcon icon={skill.icon} cellPx={3} /></span>
                <span className="skill-list-words">
                  <b>{skill.name}</b>
                  {skill.when && <small>{skill.when}</small>}
                </span>
                <span className="skill-list-state"><SkillStatusMark status={status} />{statusWord(status)}</span>
              </li>
            )
          })}
        </ul>
      </div>
    </div>,
    document.body,
  )
}

/** One card per Skill in the transcript, updated in place as its status moves. */
function SkillUseCard({ skill, use, motion }: { skill: SnapshotSkill; use: SkillUse; motion: boolean }) {
  return (
    <div className="skill-use-card" data-state={use.status} data-enter={motion && arrivedLive(use.firstSeq) ? "on" : undefined} role="note">
      <span className="skill-use-card-icon"><SkillIcon icon={skill.icon} /></span>
      <span className="skill-use-card-words">
        <b>{skill.name}</b>
        <small>{statusWord(use.status)}</small>
      </span>
    </div>
  )
}

/** The end-of-task summary: Skills used, lit, then the ones not used. */
function SkillSummaryCard({ snapshot, uses }: { snapshot: SessionSkillSnapshot; uses: ReadonlyMap<string, SkillUse> }) {
  const { used, unused } = skillSummary(snapshot, uses)
  return (
    <section className="skill-summary" aria-label={nextWord("skillSummaryTitle")}>
      <h3>{nextWord("skillSummaryTitle")}</h3>
      {used.length ? <ul className="skill-summary-used">
        {used.map((skill) => <li key={skill.id} className="skill-chip" data-state={uses.get(skill.id)?.status}>
          <span className="skill-chip-icon"><SkillIcon icon={skill.icon} /></span><span className="skill-chip-name">{skill.name}</span>
        </li>)}
      </ul> : <p>{nextWord("skillSummaryNoneUsed")}</p>}
      {unused.length > 0 && <p className="skill-summary-unused">{nextWord("skillSummaryUnused", { skills: unused.map((skill) => skill.name).join(nextWord("skillListSeparator")) })}</p>}
    </section>
  )
}

/**
 * The transcript's Skill cards, each at its first receipt's time (seconds,
 * as transcript entries are timed), and the summary at the newest end once
 * the Session's task has finished.
 */
export function transcriptSkillCards(skills: SessionSkills, finished: boolean): { at: number; node: ReactElement }[] {
  const { snapshot, uses, motion } = skills
  if (!snapshot?.skills.length) return []
  const out: { at: number; node: ReactElement }[] = []
  for (const skill of snapshot.skills) {
    const use = uses.get(skill.id)
    if (use) out.push({ at: use.firstAt / 1000, node: <SkillUseCard key={"skill-use:" + skill.id} skill={skill} use={use} motion={motion} /> })
  }
  out.sort((a, b) => a.at - b.at)
  if (finished) out.push({ at: Number.POSITIVE_INFINITY, node: <SkillSummaryCard key="skill-summary" snapshot={snapshot} uses={uses} /> })
  return out
}

/**
 * The list row's addition beside the role bot, as markup for the row's state
 * line: a Skill icon that pops out on a live receipt, or, when nothing may
 * move, the used Skills as still chips. Its canvases are painted by
 * `paintRowSkills`.
 */
export function useRowSkills(row: Pick<SessionRow, "persona" | "sessionId">, machine: string | null = null): { html: string; icons: (Icon | undefined)[] } {
  // Read with the other rows on screen, from the row's own machine (skill-feed.ts).
  const snapshot = useSessionSnapshot(row.persona ? row.sessionId : undefined, machine, false)
  const ledger = useSkillLedger(snapshot?.skills.length ? snapshot.conversationId : null, machine)
  const motion = useSkillMotion(snapshot?.scopeId, machine)
  const playing = usePlaying(snapshot, 1600)
  {
    if (!snapshot?.skills.length || !ledger) return { html: "", icons: [] }
    if (motion) {
      const skill = playing && snapshot.skills.find((entry) => entry.id === playing.receipt.skillId)
      if (!playing || !skill) return { html: "", icons: [] }
      return { icons: [skill.icon], html: '<span class="persona-skill-pop" data-seq="' + playing.receipt.seq + '" title="' +
        L.escapeHTML(skillUseLine(playing.receipt.status, skill.name)) + '"><canvas class="skill-icon" width="0" height="0" aria-hidden="true"></canvas></span>' }
    }
    const uses = ledger.usesFor(snapshot)
    const used = snapshot.skills.filter((skill) => { const status = uses.get(skill.id)?.status; return status === "read" || status === "applied" })
    if (!used.length) return { html: "", icons: [] }
    return { icons: used.map((skill) => skill.icon), html: '<span class="persona-skill-chips">' + used.map((skill) =>
      '<span class="persona-skill-chip" title="' + L.escapeHTML(skill.name + " · " + statusWord(uses.get(skill.id)?.status)) + '"><canvas class="skill-icon" width="0" height="0" aria-hidden="true"></canvas></span>').join("") + "</span>" }
  }
}

export function paintRowSkills(root: HTMLElement | null, icons: readonly (Icon | undefined)[]): void {
  const canvases = root?.querySelectorAll<HTMLCanvasElement>(".persona-skill-pop canvas, .persona-skill-chip canvas") ?? []
  canvases.forEach((canvas, index) => L.paintIcon(canvas, icons[index], 2))
}
