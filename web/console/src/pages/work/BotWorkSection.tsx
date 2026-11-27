import { useEffect, useMemo, useState } from "react"
import type * as W from "@clawdline/contract"
import { currentCatalogTag } from "../../catalog.js"
import { currentCloudMachineID } from "../../cloud/machine-switch.js"
import { BotRequestDetail, BotRequestRow, useBotRequests } from "../../project-assistants/BotRequestCard.js"
import { botOf, botRequestFetcher, boundBotRequests, readBotWorkShown, sortBotRequests, writeBotWorkShown } from "../../project-assistants/bot-request.js"
import { requestOpenBot, useBotDirectory, useBotSource } from "../../project-assistants/open-bot.js"
import { words } from "../../project-assistants/words.js"
import "./bot-work.css"

/**
 * Board → "Bot work": the Bot request cards, outside every Board column and
 * count. Shown by default; this browser remembers when it is switched off.
 * It renders nothing when the cards cannot be read here at all: an older
 * daemon, or a machine reached through Cloud below API 130. On a Cloud page
 * the cards are read and written through that machine's Bots source, the
 * same words a Bot's chat uses there.
 */
export function BotWorkSection({ project, items, onOpenItem }: {
  /** The Board's Project filter; empty for every Project. */
  project: string
  items: readonly { id: string; title: string }[]
  onOpenItem: (id: string) => void
}) {
  const w = words(currentCatalogTag()), language = currentCatalogTag()
  const bots = useBotDirectory()
  const [shown, setShown] = useState(() => readBotWorkShown())
  const machine = currentCloudMachineID(), source = useBotSource(machine)
  const fetcher = machine === null ? undefined : botRequestFetcher(source)
  const io = useMemo(() => boundBotRequests(fetcher), [source, machine])
  const { reading, refresh } = useBotRequests({ enabled: machine === null || !!fetcher, read: io.list })
  const [open, setOpen] = useState<W.BotRequestView>()
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])
  if (reading.state === "hidden") return null
  const cards = reading.state === "ready" ? sortBotRequests(reading.cards.filter((card) => !project || card.project_id === project)) : []
  const holder = open && botOf(open.baton.bot, bots)
  const linked = open?.board_item_id ? items.find((item) => item.id === open.board_item_id) : undefined
  return <section className="work-section bot-work" data-bot-work={reading.state} aria-labelledby="bot-work-title">
    <div className="work-section-head bot-work-head">
      <h2 id="bot-work-title">{w.requestSection}</h2>
      <label className="work-detail-switch bot-work-switch">
        <input type="checkbox" checked={shown} onChange={(event) => { setShown(event.currentTarget.checked); writeBotWorkShown(event.currentTarget.checked) }} />
        <span>{w.requestShow}</span>
      </label>
    </div>
    {shown && (reading.state === "loading" ? <p className="work-empty bot-work-loading" role="status">{w.loading}</p>
      : reading.state === "failed" ? <p className="work-empty" role="alert">{w.requestLoadFailed} <button type="button" className="pa-request-text-button" onClick={refresh}>{w.retry}</button></p>
        : cards.length === 0 ? <p className="work-empty">{w.requestEmpty}</p>
          : <ul className="bot-work-rows">{cards.map((card) => <li key={card.id}>
            <BotRequestRow card={card} bots={bots} w={w} now={now} onOpen={setOpen} />
          </li>)}</ul>)}
    {open && <BotRequestDetail key={open.id} card={open} bots={bots} w={w} language={language} read={io.read} write={io.write}
      onClose={() => { setOpen(undefined); refresh() }} onChanged={refresh}
      onOpenChat={holder && !holder.hidden ? () => { setOpen(undefined); requestOpenBot({ key: holder.key }) } : undefined}
      boardItem={open.board_item_id ? <button type="button" className="chip bot-work-item" onClick={() => { const id = open.board_item_id!; setOpen(undefined); onOpenItem(id) }}>
        {linked?.title ?? open.board_item_id}</button> : undefined} />}
  </section>
}
