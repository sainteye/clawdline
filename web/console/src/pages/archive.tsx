import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import type { ArchivedSession } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { catalogRefusalDetail } from "../catalog.js"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { Mark } from "../session/List.js"
import { PersonaTag, usePersonas } from "../session/PersonaBot.js"
import { sessionFragment } from "../session/address.js"
import { assistantName } from "../session/restore-offer.js"
import { archivedName, archivedWhen, entryLine, withoutRestored, type Outcome } from "./archive/model.js"
import type { PageModule } from "./types.js"
import "./archive/archive.css"

/**
 * 封存: the Sessions archived to free their memory, and the way back to each
 * (docs/session-archive.md).
 *
 * One entry per archived conversation, newest first as the daemon lists them,
 * drawn with what its row showed — the project's mark and the title in the
 * same tint — so it is recognised as the row it was. Restore is one press for
 * one conversation; it resumes through the reboot restore's path on the
 * machine, and an entry leaves the list only when its conversation opened.
 *
 * Every read and press goes through `client`, which Clawdline Cloud answers
 * from the relay (`archived-sessions`, `restore-archived`), so the page is the
 * same on the phone as on the machine.
 */

type ListState =
  | { read: false; failure?: string }
  | { read: true; sessions: ArchivedSession[] }

/** One conversation that came back, and the Session it came back as when the machine said. */
interface Reopened {
  conversation: string
  name: string
  id?: string
}

let minted = 0
/** One key per press: the restore's Idempotency-Key. As `press-holds.ts`, unrepeated rather than unguessable. */
function pressKey(): string {
  const c = globalThis.crypto
  if (typeof c?.randomUUID === "function") {
    try {
      return c.randomUUID()
    } catch {
      /* below */
    }
  }
  minted += 1
  return "archive-restore-" + minted + "-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2)
}

function language(): string | undefined {
  return typeof document === "undefined" ? undefined : document.documentElement.lang || undefined
}

function ArchivePageView({ shown }: { shown: boolean }) {
  const T = L.strings
  const [list, setList] = useState<ListState>({ read: false })
  const [busy, setBusy] = useState(false)
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({})
  const [reopened, setReopened] = useState<Reopened[]>([])
  const [unknown, setUnknown] = useState(false)
  const ticket = useRef(0)
  const title = useRef<HTMLHeadingElement>(null)
  const personas = usePersonas()

  const load = useCallback(async () => {
    const mine = ++ticket.current
    setBusy(true)
    try {
      const answer = await client.archived()
      if (mine !== ticket.current) return
      setList({ read: true, sessions: answer.sessions ?? [] })
    } catch (e) {
      if (mine !== ticket.current) return
      setList({ read: false, failure: L.failureSentence(e, nextWord("archiveUnreadable")) })
    } finally {
      if (mine === ticket.current) setBusy(false)
    }
  }, [])

  // Read on every arrival: an archive made from the list a moment ago is the
  // likeliest thing to look for here.
  useEffect(() => {
    if (!shown) return
    setReopened([])
    setUnknown(false)
    void load()
  }, [shown, load])

  useLayoutEffect(() => {
    if (shown) title.current?.focus({ preventScroll: true })
  }, [shown])

  const restore = async (s: ArchivedSession) => {
    const conversation = s.conversation_id
    if (outcomes[conversation]?.kind === "restoring") return
    setOutcomes((o) => ({ ...o, [conversation]: { kind: "restoring" } }))
    setUnknown(false)
    try {
      const answer = await client.restoreArchived([conversation], pressKey())
      const result = (answer.results ?? []).find((r) => r.conversation_id === conversation)
      if (result?.ok) {
        setOutcomes(({ [conversation]: _, ...rest }) => rest)
        setList((l) => (l.read ? { read: true, sessions: withoutRestored(l.sessions, conversation) } : l))
        setReopened((r) => [{ conversation, name: archivedName(s), id: result.id }, ...r])
        return
      }
      setOutcomes((o) => ({ ...o, [conversation]: { kind: "failed", code: result?.code, message: result?.message, messageLang: result?.message ? "en" : undefined } }))
    } catch (e) {
      if (e instanceof RefusalError) {
        const detail = catalogRefusalDetail(e)
        setOutcomes((o) => ({ ...o, [conversation]: { kind: "failed", code: e.code, message: detail?.text || e.code, messageLang: detail?.lang || "en" } }))
        return
      }
      // No answer: whether it opened is not known, so the list is read again
      // rather than guessed at.
      setOutcomes(({ [conversation]: _, ...rest }) => rest)
      setUnknown(true)
      void load()
    }
  }

  const open = (id: string) => {
    // The address is how a Session is opened from anywhere (`App`'s
    // `routeTo`), and it waits for the list to have the new row.
    location.hash = sessionFragment(id)
  }

  const now = Date.now()
  return (
    <section
      id="archive"
      className="page board-page archive-page"
      data-page-view="archive"
      hidden={!shown}
      aria-labelledby="archive-title"
    >
      <header className="board-head">
        <div className="work-head-tools">
          <button className="board-button" id="archive-refresh" type="button" disabled={busy} onClick={() => void load()}>
            {busy ? T.webLoading : nextWord("archiveRefresh")}
          </button>
        </div>
      </header>
      <div className="archive-wrap">
        <div className="board-intro">
          <p className="board-eyebrow">{nextWord("archiveEyebrow")}</p>
          <h1 id="archive-title" tabIndex={-1} ref={title}>{nextWord("archivePageTitle")}</h1>
          <p className="archive-lede">{nextWord("archiveLede")}</p>
        </div>
        {reopened.length > 0 && (
          <ul className="archive-reopened" role="status">
            {reopened.map((r) => (
              <li key={r.conversation}>
                <span className="archive-reopened-name">{r.name}</span>
                <span>{nextWord("archiveRestored")}</span>
                {r.id ? (
                  <button className="board-button" type="button" onClick={() => open(r.id!)}>
                    {nextWord("archiveOpenSession")}
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
        {unknown && <p className="archive-failure" role="status">{nextWord("archiveUnknownOutcome")}</p>}
        {!list.read ? (
          list.failure ? (
            <p className="archive-failure" role="status">{list.failure}</p>
          ) : (
            <p className="archive-empty" role="status">{nextWord("archiveLoading")}</p>
          )
        ) : list.sessions.length === 0 ? (
          <p className="archive-empty">{nextWord("archiveEmpty")}</p>
        ) : (
          <ul className="archive-list">
            {list.sessions.map((s) => {
              const name = archivedName(s)
              const when = archivedWhen(s.archived_at, now, language())
              const outcome = outcomes[s.conversation_id]
              const line = entryLine(outcome, (key, holes) => nextWord(key, holes))
              const message = outcome?.kind === "failed" ? outcome.message : undefined
              const messageAt = message && outcome?.kind === "failed" && outcome.messageLang === "en" ? line.indexOf(message) : -1
              const where = L.path(s.cwd) || s.place_label
              return (
                <li className="archive-entry" key={s.conversation_id} data-conversation={s.conversation_id}>
                  <Mark icon={s.icon} cellPx={4} />
                  <div className="archive-copy">
                    <b className="archive-title" style={{ color: L.accentTint(s.icon?.accent) }}>{name}</b>
                    <span className="archive-where">
                      <span className="archive-path">{where}</span>
                      <span className="archive-assistant">{assistantName(s.assistant)}</span>
                      <PersonaTag id={s.persona} personas={personas} />
                    </span>
                    <time className="archive-when" dateTime={s.archived_at > 0 ? new Date(s.archived_at * 1000).toISOString() : undefined} title={when.absolute}>
                      {nextWord("archivedAt", { relative: when.relative, when: when.absolute })}
                    </time>
                    {line ? (
                      <p className="archive-outcome" data-failed={outcome?.kind === "failed" || undefined} role="status">
                        {messageAt >= 0 && message ? <>{line.slice(0, messageAt)}<span lang="en">{message}</span>{line.slice(messageAt + message.length)}</> : line}
                      </p>
                    ) : null}
                  </div>
                  <button
                    className="board-button archive-restore"
                    type="button"
                    disabled={outcome?.kind === "restoring"}
                    aria-label={nextWord("archiveRestore") + " " + name}
                    onClick={() => void restore(s)}
                  >
                    {outcome?.kind === "restoring" ? nextWord("archiveRestoring") : nextWord("archiveRestore")}
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </section>
  )
}

export const page: PageModule = { id: "archive", Component: ArchivePageView }
