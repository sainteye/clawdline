import { useEffect, useRef, useState } from "react"
import { clock, strings } from "../../legacy/bridge.js"
import { catalogWord } from "../../catalog.js"
import { nextWord } from "../../next-strings.js"
import { LoadingSkeleton } from "../../LoadingSkeleton.js"
import type { ProjectPlace } from "../work/api.js"
import { readProjectDevStack, type ProjectDevStackReply } from "./project-devstack-api.js"
import "./project-devstack.css"

const LINK_STATES: Record<string, string> = {
  ok: "webLinkOk",
  fail: "webLinkFail",
  down: "webLinkDown",
  running: "webLinkRunning",
}

function openable(url: string): boolean {
  return /^https?:\/\//i.test(url || "")
}

export function ProjectDevStack({ place, onAsk }: { place: ProjectPlace; onAsk(request: string): void }) {
  const [data, setData] = useState<ProjectDevStackReply | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const serial = useRef(0)

  const reload = async () => {
    const ticket = ++serial.current
    setLoading(true)
    setError("")
    try {
      const reply = await readProjectDevStack(place.id)
      if (ticket === serial.current) setData(reply)
    } catch (err: any) {
      if (ticket === serial.current) {
        setError(err.message || catalogWord("literal", "7f523f32a2c0"))
      }
    } finally {
      if (ticket === serial.current) setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
    return () => {
      serial.current++
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [place.id])

  if (loading && !data) {
    return (
      <div className="project-devstack" role="status" aria-busy="true">
        <LoadingSkeleton id="SK-DEVSTACK" kind="cards" label={catalogWord("inline", "db8653492c3a")} />
      </div>
    )
  }

  if (error && !data) {
    return (
      <div className="project-devstack">
        <p role="alert">
          {error}{" "}
          <button type="button" onClick={() => void reload()}>
            {nextWord("devStackRefresh")}
          </button>
        </p>
      </div>
    )
  }

  const devStack = data?.devStack || "missing"
  const links = data?.links || []
  const when = data?.observedAt ? clock(data.observedAt) : ""

  const statusText =
    devStack === "ready"
      ? nextWord("devStackStatusReady", { count: data?.serverCount ?? 0 })
      : devStack === "attention"
        ? catalogWord("literal", "3723c98b41df")
        : devStack === "empty"
          ? catalogWord("literal", "f1c62f33216d")
          : catalogWord("literal", "c1a43ec0edc2")

  return (
    <div className="project-devstack">
      <div className="project-devstack-head">
        <div>
          <h3>{nextWord("devStackSectionTitle")}</h3>
          <p>{nextWord("devStackSectionDesc")}</p>
        </div>
        <div className="project-devstack-actions">
          <button type="button" disabled={loading} onClick={() => void reload()}>
            {nextWord("devStackRefresh")}
          </button>
        </div>
      </div>

      <div className={`project-devstack-status-card is-${devStack}`} role="region" aria-label={statusText}>
        <div className="project-devstack-status-info">
          <span className="project-devstack-dot" aria-hidden="true" />
          <span>{statusText}</span>
        </div>
        <div className="project-devstack-actions">
          {devStack === "ready" && (
            <>
              <button
                type="button"
                className="is-primary"
                onClick={() => onAsk(nextWord("stackStartRequest"))}
              >
                {nextWord("stackStartButton")}
              </button>
              <button
                type="button"
                onClick={() => onAsk(nextWord("devStackEditRequest"))}
              >
                {nextWord("devStackEditButton")}
              </button>
            </>
          )}
          {(devStack === "missing" || devStack === "attention" || devStack === "empty") && (
            <button
              type="button"
              className="is-primary"
              onClick={() => onAsk(nextWord("stackCreateRequest"))}
            >
              {nextWord("stackCreateButton")}
            </button>
          )}
        </div>
      </div>

      <section className="project-devstack-section" aria-labelledby="project-devstack-links-title">
        <div className="project-devstack-section-title">
          <h4 id="project-devstack-links-title">{nextWord("devStackLinksTitle")}</h4>
          {when && <span className="project-devstack-when">{when}</span>}
        </div>
        {links.length > 0 ? (
          <div className="project-devstack-links">
            {links.map((link, idx) => {
              const word =
                link.status ||
                (LINK_STATES[link.state] ? strings[LINK_STATES[link.state]] : "") ||
                (link.unknownReason === "status_not_run"
                  ? nextWord("stackStatusNotRun")
                  : link.unknownReason === "nothing_declared"
                    ? nextWord("stackNothingDeclared")
                    : "")
              const url = String(link.url || "")
              const where = url.replace(/^https?:\/\//i, "")
              const inner = (
                <>
                  <span className="dot" aria-hidden="true" />
                  <span className="lbl">{link.label || link.kind}</span>
                  {word && <span className="st">{word}</span>}
                  <span className="host" title={url}>
                    {where} {openable(url) ? "↗" : ""}
                  </span>
                </>
              )
              return (
                <div key={idx} className="dep-row">
                  {openable(url) ? (
                    <a
                      className="dep"
                      data-state={link.state || ""}
                      href={url}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      {inner}
                    </a>
                  ) : (
                    <div className="dep" data-state={link.state || ""}>
                      {inner}
                    </div>
                  )}
                  {link.why && <p className="dep-note">{link.why}</p>}
                </div>
              )
            })}
          </div>
        ) : (
          <p className="project-devstack-empty">{nextWord("devStackLinksEmpty")}</p>
        )}
      </section>

      <section className="project-devstack-section" aria-labelledby="project-devstack-config-title">
        <div className="project-devstack-config-head">
          <h4 id="project-devstack-config-title">{nextWord("devStackConfigTitle")}</h4>
          <p>{nextWord("devStackConfigNote")}</p>
        </div>
        {data?.raw ? (
          <pre className="project-devstack-raw" tabIndex={0}>
            <code>{data.raw}</code>
          </pre>
        ) : (
          <div className="project-devstack-missing-note">
            <p>{nextWord("devStackConfigMissing")}</p>
          </div>
        )}
      </section>
    </div>
  )
}
