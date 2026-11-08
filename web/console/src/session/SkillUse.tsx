/*
 * Where a Session shows its role using a Skill: the role bot in the detail
 * header (a toggle for the Skill strip, and a hop on `applied`), the strip of
 * this Session's own Skills, a card in the transcript per Skill, a summary
 * when the Session's task has finished, and a small icon beside the bot on a
 * list row. Data: skill-feed.ts; rules: skill-use.ts.
 */
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactElement } from "react"
import type { Icon, Persona, SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { PersonaBot } from "./PersonaBot.js"
import { arrivedLive, useSessionSnapshot, useSkillLedger, useSkillMoments, useSkillMotion } from "./skill-feed.js"
import { MomentQueue, receiptBelongs, skillSummary, type SessionSkillSnapshot, type SkillMoment, type SkillUse, type SkillUseStatus, type SnapshotSkill } from "./skill-use.js"
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
  const ledger = useSkillLedger(!!snapshot?.skills.length)
  const motion = useSkillMotion(snapshot?.scopeId)
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
  useSkillMoments(!!snapshot?.skills.length, listener)
  return playing
}

/** Whether a Session's strip is open: opened by its first live receipt, or by the toggle. */
const stripChoice = new Map<string, boolean>()

export interface DetailSkills extends SessionSkills {
  playing: SkillMoment | null
  open: boolean
  entering: boolean
  toggle: () => void
}

export function useDetailSkills(row: SessionRow | null, read: boolean): DetailSkills {
  const skills = useSessionSkills(row, read)
  const { snapshot, uses } = skills
  const playing = usePlaying(snapshot)
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
  return { ...skills, playing, open: !!key && !!stripChoice.get(key), entering: entering && skills.motion, toggle }
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
    </button>
  )
}

/** The strip of this Session's Skills: used ones lit, unused dimmed, the latest use said in words. */
export function SkillStrip({ skills }: { skills: DetailSkills }) {
  const { snapshot, uses, playing, motion } = skills
  if (!snapshot?.skills.length || !skills.open) return null
  const latest = [...uses.values()].sort((a, b) => b.seq - a.seq)[0]
  const said = playing ? { skillId: playing.receipt.skillId, status: playing.receipt.status } : latest
  const saidSkill = said && snapshot.skills.find((skill) => skill.id === said.skillId)
  return (
    <div className="skill-strip" id="skill-strip" data-enter={skills.entering ? "on" : undefined}>
      <ul className="skill-strip-list" aria-label={nextWord("skillStripLabel")}>
        {snapshot.skills.map((skill) => {
          const status = uses.get(skill.id)?.status
          const play = motion && playing?.receipt.skillId === skill.id ? playing.receipt.status : undefined
          return (
            <li key={skill.id} className="skill-chip" data-state={status ?? "unused"} title={skill.name + " · " + statusWord(status)}>
              <span className="skill-chip-icon" key={play ? playing!.receipt.seq : 0} data-play={play}>
                <SkillIcon icon={skill.icon} />
              </span>
              <span className="skill-chip-name">{skill.name}</span>
              {status === "failed" && <span className="skill-chip-failed" aria-hidden="true" />}
              <span className="sr-only skill-chip-status">{statusWord(status)}</span>
            </li>
          )
        })}
      </ul>
      <p className="skill-strip-said" role="status" aria-live="polite">{said && saidSkill ? skillUseLine(said.status, saidSkill.name) : ""}</p>
    </div>
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
export function useRowSkills(row: Pick<SessionRow, "persona" | "sessionId">): { html: string; icons: (Icon | undefined)[] } {
  const snapshot = useSessionSnapshot(row.persona ? row.sessionId : undefined)
  const ledger = useSkillLedger(!!snapshot?.skills.length)
  const motion = useSkillMotion(snapshot?.scopeId)
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
