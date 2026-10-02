import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { sessionsPageHash, terminalRouteFromHash, workPageHash, workProjectID, type TerminalRoute } from "../page-route.js"
import { requestPage } from "../overlays/index.js"
import { readProjectPlaces, type ProjectPlacePage } from "./work/api.js"
import { watchTerminalHost, type TerminalHost } from "../cloud/terminal-host.js"
import { CloudProjectReader, resolveCloudTerminalProject, type ProjectReadState } from "./terminal/cloud-project.js"
import { hostedConsole } from "./terminal/api.js"
import { TERMINAL_ROUTE, openTerminalPage } from "./terminal/navigate.js"
import { TerminalProjectList } from "./terminal/TerminalProjectList.js"
import { TerminalView } from "./terminal/TerminalView.js"
import { CloudTerminalPage } from "./terminal/CloudTerminalPage.js"
import type { PageModule } from "./types.js"
import "./terminal/terminal.css"

/**
 * 終端: ordinary shells on this machine's own tmux server, opened from a
 * project (plan v3 §6 F3; the routes are api/v1/terminals.schema.json).
 *
 * `#page=terminal&project=<place id>` lists that project's terminals;
 * `&terminal=<id>` shows one. The page has no drawer entry: it is reached
 * from the Projects page and the work page's project scope, and `&from=`
 * says which, so the list's Back returns there (an address without it goes
 * to the board, as every address did before).
 *
 * The hosted branch uses its dedicated encrypted terminal channels. The local
 * branch keeps its existing fetch/SSE transport. The hosted address carries the
 * Cloud Project id; the channel is asked by the machine-local one, resolved
 * only for a listed row of the terminal host's own machine (cloud-project.ts).
 */

function currentRoute(): TerminalRoute {
  return terminalRouteFromHash(typeof location === "undefined" ? "" : location.hash)
}

function TerminalPage({ shown }: { shown: boolean }) {
  const [route, setRoute] = useState<TerminalRoute>(currentRoute)
  // The address may name the project by the work page's id or by its folder
  // (the Projects page links by folder); the terminal routes take the id.
  const [place, setPlace] = useState<{ asked: string; id: string; label: string } | "loading" | "failed">("loading")
  const [again, setAgain] = useState(0)
  const title = useRef<HTMLHeadingElement>(null)
  const hosted = hostedConsole()
  const [host, setHost] = useState<TerminalHost | null>(null)
  const [cloud, setCloud] = useState<ProjectReadState<ProjectPlacePage>>({ state: "loading" })
  const reader = useRef<CloudProjectReader<ProjectPlacePage> | null>(null)

  useEffect(() => {
    const read = () => setRoute(currentRoute())
    if (shown) read()
    window.addEventListener("hashchange", read)
    document.addEventListener(TERMINAL_ROUTE, read)
    return () => {
      window.removeEventListener("hashchange", read)
      document.removeEventListener(TERMINAL_ROUTE, read)
    }
  }, [shown])

  useEffect(() => (hosted ? watchTerminalHost(setHost) : undefined), [hosted])

  // Hosted: read the Cloud list, retry on a bounded schedule, and read again at
  // once when the terminal host comes back. A new route starts a new reader.
  useEffect(() => {
    if (!hosted || !route.project) return
    const next = new CloudProjectReader(readProjectPlaces, setCloud)
    reader.current = next
    next.start()
    return () => { next.dispose(); if (reader.current === next) reader.current = null }
  }, [hosted, route.project])
  useEffect(() => { reader.current?.host(host) }, [host, route.project])

  useEffect(() => {
    if (hosted || !route.project) return
    let live = true
    setPlace((was) => (was !== "loading" && was !== "failed" && was.asked === route.project ? was : "loading"))
    readProjectPlaces().then(
      (page) => {
        if (!live) return
        const id = workProjectID(route.project, page.places)
        const found = page.places.find((p) => p.id === id)
        setPlace({ asked: route.project, id, label: found?.label ?? "" })
      },
      () => live && setPlace("failed"),
    )
    return () => {
      live = false
    }
  }, [route.project, hosted, again])

  useEffect(() => {
    if (shown && !route.terminal) title.current?.focus({ preventScroll: true })
  }, [shown, route.terminal])

  const known = typeof place === "object" && place.asked === route.project ? place : null
  const resolved = hosted && cloud.state === "ready" && host ? resolveCloudTerminalProject(route.project, cloud.page.places, host.machine) : null
  const found = resolved?.kind === "found" ? resolved : null
  const project = hosted ? found?.page ?? "" : known?.id ?? ""
  const name = (hosted ? found?.label : known?.label) || project || route.project
  const toProjects = route.from === "projects"
  const back = () => {
    if (route.from === "sessions") {
      try { history.replaceState(history.state, "", sessionsPageHash(true)) } catch { location.hash = sessionsPageHash(true) }
      requestPage({ page: "sessions", hash: false })
      window.dispatchEvent(new HashChangeEvent("hashchange"))
      return
    }
    if (toProjects) {
      requestPage({ page: "projects" })
      return
    }
    try {
      history.replaceState(history.state, "", workPageHash(project))
    } catch {
      // refusal-ok: leaving the page still works; only the address is not rewritten.
    }
    requestPage({ page: "work", hash: false })
  }
  const backButton = (
    <button className="board-button" type="button" onClick={back}>
      {route.from === "sessions" ? nextWord("terminalBackSessions") : nextWord(toProjects ? "terminalBackProjects" : "terminalBackBoard")}
    </button>
  )

  return (
    <section
      id="terminal"
      className="page board-page terminal-page"
      data-page-view="terminal"
      hidden={!shown}
      aria-labelledby="terminal-title"
      data-view={route.terminal ? "one" : "list"}
    >
      {hosted ? (
        <>
          <header className="board-head terminal-page-head">
            <h1 id="terminal-title" ref={title} tabIndex={-1}>{route.terminal ? nextWord("terminalEntryFor", { project: name }) : nextWord("terminalEntry")}</h1>
            {!route.terminal && backButton}
          </header>
          {!route.project ? <p className="terminal-note" role="note">{nextWord("terminalCloudChooseProject")}</p>
            : cloud.state === "failed" ? <div className="terminal-note-row">
              <p className="terminal-note" role="alert">{cloud.temporary ? nextWord("terminalProjectsReconnecting") : nextWord("terminalProjectsFailedCode", { code: cloud.code })}
                {" "}{cloud.retryInMs !== null ? nextWord("terminalProjectsRetryIn", { seconds: String(Math.round(cloud.retryInMs / 1000)) }) : nextWord("terminalProjectsRetryStopped")}</p>
              <button className="board-button" type="button" onClick={() => reader.current?.retry()}>{nextWord("terminalRetry")}</button></div>
            : cloud.state === "loading" ? <p className="terminal-note">{nextWord("terminalListLoading")}</p>
              : !host ? <p className="terminal-note" role="status">{nextWord("terminalCloudOffline")}</p>
                : !found ? <p className="terminal-note" role="alert">{nextWord("terminalProjectUnknown")}</p>
                  : <CloudTerminalPage project={found.page} channelProject={found.local} label={name} id={route.terminal} shown={shown} from={route.from} />}
        </>
      ) : route.terminal ? (
        <>
          <h1 id="terminal-title" className="terminal-sr">
            {nextWord("terminalEntryFor", { project: name })}
          </h1>
          <TerminalView id={route.terminal} shown={shown} label={name}
            onBack={() => openTerminalPage(project || route.project, "", route.from)}
            onOpenNew={(id) => openTerminalPage(project || route.project, id, route.from)} />
        </>
      ) : (
        <div className="terminal-wrap">
          <header className="board-head terminal-page-head">
            <h1 id="terminal-title" ref={title} tabIndex={-1}>{nextWord("terminalEntry")}</h1>
            {backButton}
          </header>
          {!route.project
            ? <p className="terminal-note" role="note">{nextWord("terminalNoProject")}</p>
            : place === "failed"
              ? (
                <div className="terminal-note-row">
                  <p className="terminal-note" role="alert">{nextWord("terminalProjectsFailed")}</p>
                  <button className="board-button" type="button" onClick={() => setAgain((n) => n + 1)}>{nextWord("terminalRetry")}</button>
                </div>
              )
              : !known
                ? <p className="terminal-note">{nextWord("terminalListLoading")}</p>
                : !project
                  ? <p className="terminal-note" role="alert">{nextWord("terminalProjectUnknown")}</p>
                  : <TerminalProjectList project={project} label={name} shown={shown} from={route.from} />}
        </div>
      )}
    </section>
  )
}

export const page: PageModule = { id: "terminal", drawer: false, Component: TerminalPage }
