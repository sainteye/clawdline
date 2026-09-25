import { useEffect, useRef, useState, type RefObject } from "react"
import type { UsageBill, UsageComposition, UsageGap, UsageItem, UsageSession, UsageTokens } from "@clawdline/contract"
import { readItemUsage, readSessionUsage } from "./api.js"
import { failureWords, when } from "./shared.js"
import { language, workWord } from "./words.js"
import {
  UsageCache,
  billCounted,
  cardLine,
  formatShare,
  formatTokens,
  gapWords,
  itemAbove,
  moneyOf,
  reasonWords,
  sessionReasonWords,
  shareWords,
  shortId,
} from "./token-bill.js"
import "./token-bill.css"

/*
 * The token ledger's bills on the Board card, the item detail and the session
 * detail (docs/token-ledger.md "What a person and a session see"). Every view
 * of one item reads one cached answer; a card asks only once it is on screen.
 */

const items = new UsageCache<UsageItem>(readItemUsage)
const sessions = new UsageCache<UsageSession>(readSessionUsage)

type Reading<T> = { phase: "idle" | "loading" } | { phase: "ready"; value: T } | { phase: "failed"; words: string }

/** One cached reading of `id`, asked for when `wanted`, and again when `version` changes. */
function useUsage<T>(cache: UsageCache<T>, id: string, wanted: boolean, version: unknown = 0) {
  const [reading, setReading] = useState<Reading<T>>({ phase: "idle" })
  const [ask, setAsk] = useState(0)
  useEffect(() => {
    if (!id || !wanted) return
    let live = true
    setReading((was) => was.phase === "ready" ? was : { phase: "loading" })
    cache.get(id, ask > 0).then(
      (value) => { if (live) setReading({ phase: "ready", value }) },
      (e: unknown) => { if (live) setReading({ phase: "failed", words: failureWords(e) }) },
    )
    return () => { live = false }
  }, [cache, id, wanted, version, ask])
  return { reading, retry: () => setAsk((n) => n + 1) }
}

/** Whether the element has come on screen once; true at once where nothing can tell. */
function useSeen<E extends Element>(): [RefObject<E | null>, boolean] {
  const ref = useRef<E>(null)
  const [seen, setSeen] = useState(false)
  useEffect(() => {
    if (seen) return
    const el = ref.current
    if (!el || typeof IntersectionObserver === "undefined") { setSeen(true); return }
    const watch = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) { setSeen(true); watch.disconnect() }
    }, { rootMargin: "200px" })
    watch.observe(el)
    return () => watch.disconnect()
  }, [seen])
  return [ref, seen]
}

/**
 * A Board card's cost line, "$6.87 · impl 70% · harness 18%", opening onto the
 * item's whole bill. Nothing at all until the bill counted something: a card
 * never says $0 for a bill nobody has read.
 */
export function ItemUsageCard({ itemId, version }: { itemId: string; version: number }) {
  const [ref, seen] = useSeen<HTMLDivElement>()
  const { reading } = useUsage(items, itemId, seen, version)
  const line = reading.phase === "ready" ? cardLine(reading.value.bill) : null
  return <div ref={ref} className="work-usage-slot">
    {line && reading.phase === "ready" && <details className="work-usage">
      <summary title={workWord("usageCardLabel")} aria-label={`${workWord("usageTitle")}: ${line}`}>{line}</summary>
      <ItemUsageBody usage={reading.value} />
    </details>}
  </div>
}

/** An item's whole bill, as the item detail shows it: read when the detail opens. */
export function ItemUsageDetail({ itemId, version }: { itemId: string; version?: number }) {
  const { reading, retry } = useUsage(items, itemId, true, version)
  return <section className="work-usage-detail" aria-label={workWord("usageTitle")}>
    <h3>{workWord("usageTitle")}</h3>
    <ReadingState reading={reading} retry={retry} />
    {reading.phase === "ready" && <ItemUsageBody usage={reading.value} />}
  </section>
}

/**
 * A session's bill, folded in the session detail and read the first time it
 * is opened.
 */
export function SessionUsage({ conversation }: { conversation: string }) {
  const [open, setOpen] = useState(false)
  const { reading, retry } = useUsage(sessions, conversation, open)
  const ready = reading.phase === "ready" ? reading.value : null
  const line = ready && !ready.reason ? cardLine(ready.bill) : null
  return <details className="work-usage-detail session-usage" onToggle={(ev) => setOpen((ev.currentTarget as HTMLDetailsElement).open)}>
    <summary><b>{workWord("usageTitle")}</b>{line && <span>{line}</span>}</summary>
    <ReadingState reading={reading} retry={retry} />
    {ready && <SessionUsageBody usage={ready} />}
  </details>
}

function ReadingState<T>({ reading, retry }: { reading: Reading<T>; retry: () => void }) {
  if (reading.phase === "loading") return <p className="work-usage-note" role="status">{workWord("usageLoading")}</p>
  if (reading.phase !== "failed") return null
  return <p className="work-usage-note" role="alert">
    {workWord("usageUnreadable", { reason: reading.words })}{" "}
    <button className="chip" type="button" onClick={retry}>{workWord("usageRetry")}</button>
  </p>
}

export function ItemUsageBody({ usage }: { usage: UsageItem }) {
  const lang = language()
  const { calls, above } = itemAbove(usage)
  const counted = billCounted(usage.bill)
  return <div className="work-usage-body">
    {counted ? <>
      <Stats parts={[
        moneyOf(usage.bill.total, lang),
        workWord("usageTokens", { n: formatTokens(usage.bill.total.total) }),
        workWord("usageCalls", { n: usage.calls }),
        workWord("usagePeak", { n: formatTokens(usage.peak_context) }),
        aboveWords(calls, above),
      ]} />
      <BillTable bill={usage.bill} />
      <p className="work-usage-note">{workWord("usageItemWhole")}</p>
    </> : <p className="work-usage-note">
      {usage.sessions.length || usage.tasks.length || usage.gaps.length ? workWord("usageNothingCounted") : workWord("usageNoOwners")}
    </p>}
    {usage.sessions.length > 0 && <Included title={workWord("usageSessions")} rows={usage.sessions.map((s) => ({
      id: s.conversation,
      name: `${s.assistant ? s.assistant + " " : ""}${shortId(s.conversation)}`,
      said: sessionRowWords(s),
    }))} />}
    {usage.tasks.length > 0 && <Included title={workWord("usageTasks")} rows={usage.tasks.map((t) => ({
      id: t.task_id,
      name: shortId(t.task_id),
      said: t.reason ? reasonWords(t.reason, lang) : billWords(t.bill, t.calls),
    }))} />}
    <Gaps gaps={usage.gaps} />
  </div>
}

export function SessionUsageBody({ usage }: { usage: UsageSession }) {
  const lang = language()
  const why = sessionReasonWords(usage, lang)
  const counted = billCounted(usage.bill)
  return <div className="work-usage-body">
    {why && <p className="work-usage-note" role="status">{why}</p>}
    {counted && <>
      <Stats parts={[
        moneyOf(usage.bill.total, lang),
        workWord("usageTokens", { n: formatTokens(usage.bill.total.total) }),
        workWord("usageCalls", { n: usage.calls }),
        workWord("usagePeak", { n: formatTokens(usage.peak_context) }),
        aboveWords(usage.calls_above, usage.above),
        usage.compactions ? workWord("usageCompactions", { n: usage.compactions }) : "",
        usage.subagents.length ? workWord("usageSubagents", { n: usage.subagents.length }) : "",
        usage.read_at ? workWord("usageReadAt", { time: when(usage.read_at) }) : "",
      ]} />
      {usage.more && <p className="work-usage-note">{workWord("usageMore")}</p>}
      <BillTable bill={usage.bill} />
    </>}
    {usage.reason !== "not_yet_read" && <Composition composition={usage.composition} />}
    <Gaps gaps={usage.gaps} />
  </div>
}

function aboveWords(calls: number, above: UsageTokens): string {
  if (!calls) return workWord("usageAboveNone")
  return workWord("usageAbove", { n: calls, cost: moneyOf(above, language()) })
}

function billWords(bill: UsageBill, calls: number): string {
  if (!billCounted(bill)) return workWord("usageNothingCounted")
  return [moneyOf(bill.total, language()), workWord("usageCalls", { n: calls })].join(" · ")
}

function sessionRowWords(s: UsageSession): string {
  const why = sessionReasonWords(s, language())
  if (s.reason === "not_yet_read") return why ?? ""
  return [billWords(s.bill, s.calls), why].filter(Boolean).join(" · ")
}

function Stats({ parts }: { parts: string[] }) {
  return <p className="work-usage-stats">{parts.filter(Boolean).map((p) => <span key={p}>{p}</span>)}</p>
}

/** Every category in the ledger's order: share, tokens and cost; rules marked as the upper bound it is. */
function BillTable({ bill }: { bill: UsageBill }) {
  const lang = language()
  return <>
    <table className="work-usage-table">
      <thead><tr>
        <th scope="col">{workWord("usageColCategory")}</th>
        <th scope="col">{workWord("usageColShare")}</th>
        <th scope="col">{workWord("usageColTokens")}</th>
        <th scope="col">{workWord("usageColCost")}</th>
      </tr></thead>
      <tbody>{bill.categories.map((c) => <tr key={c.name} data-empty={c.tokens.total > 0 ? undefined : ""}>
        <th scope="row">{c.name}{c.upper_bound && <small className="work-usage-bound" title={workWord("usageRulesWhy")}>
          {" "}{workWord("usageUpperBound")}</small>}</th>
        <td>{shareWords(c)}</td>
        <td>{formatTokens(c.tokens.total)}</td>
        <td>{moneyOf(c.tokens, lang)}</td>
      </tr>)}</tbody>
    </table>
    {bill.categories.some((c) => c.upper_bound && c.tokens.total > 0) &&
      <p className="work-usage-note">rules · {workWord("usageRulesWhy")}</p>}
    {bill.share_of === "tokens" && <p className="work-usage-note">{workWord("usageShareOfTokens")}</p>}
  </>
}

function Included({ title, rows }: { title: string; rows: { id: string; name: string; said: string }[] }) {
  return <div className="work-usage-included">
    <h4>{title}</h4>
    <ul>{rows.map((r) => <li key={r.id} title={r.id}><code>{r.name}</code> {r.said}</li>)}</ul>
  </div>
}

/** What could not be read, each named: a bill never leaves something out without saying so. */
function Gaps({ gaps }: { gaps: UsageGap[] }) {
  if (!gaps.length) return null
  const lang = language()
  return <div className="work-usage-included work-usage-gaps" role="note">
    <h4>{workWord("usageGaps")}</h4>
    <ul>{gaps.map((g) => <li key={`${g.kind}:${g.id}`} title={g.id}>{gapWords(g, lang)}</li>)}</ul>
  </div>
}

/** The base every call pays again, when the transcript recorded it; said to be unknown otherwise. */
function Composition({ composition }: { composition?: UsageComposition }) {
  if (!composition) return <div className="work-usage-included"><h4>{workWord("usageBase")}</h4>
    <p className="work-usage-note">{workWord("usageBaseUnknown")}</p></div>
  const whole = composition.measured || 1
  const sum = (rows: { tokens: number }[]) => rows.reduce((n, r) => n + r.tokens, 0)
  const rows: { name: string; tokens: number; parts?: { name: string; tokens: number }[] }[] = [
    { name: workWord("usageBaseSystemPrompt"), tokens: composition.system_prompt },
    { name: workWord("usageBaseTools"), tokens: sum(composition.tools), parts: composition.tools },
    { name: workWord("usageBaseSkills"), tokens: composition.skill_listing },
    { name: workWord("usageBaseInstructions"), tokens: sum(composition.instructions), parts: composition.instructions },
    { name: workWord("usageBaseMcp"), tokens: composition.mcp_instructions },
    { name: workWord("usageBaseOther"), tokens: composition.other },
  ]
  return <div className="work-usage-included work-usage-base">
    <h4>{workWord("usageBase")}</h4>
    <p className="work-usage-note">{workWord("usageBaseMeasured", { n: formatTokens(composition.measured) })}</p>
    <ul>{rows.map((r) => <li key={r.name}>
      <span>{r.name}</span> {formatTokens(r.tokens)} · {formatShare(r.tokens / whole)}
      {!!r.parts?.length && <ul>{r.parts.map((p) => <li key={p.name}><code>{p.name}</code> {formatTokens(p.tokens)}</li>)}</ul>}
    </li>)}</ul>
  </div>
}
