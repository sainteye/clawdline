import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { terminalRouteFromHash, workPageHash, workProjectID, type TerminalRoute } from "../page-route.js"
import { requestPage } from "../overlays/index.js"
import { readProjectPlaces } from "./work/api.js"
import { hostedConsole } from "./terminal/api.js"
import { TERMINAL_ROUTE, openTerminalPage } from "./terminal/navigate.js"
import { TerminalProjectList } from "./terminal/TerminalProjectList.js"
import { TerminalView } from "./terminal/TerminalView.js"
import type { PageModule } from "./types.js"
import "./terminal/terminal.css"

/**
 * 終端: ordinary shells on this machine's own tmux server, opened from a
 * project (plan v3 §6 F3; the routes are api/v1/terminals.schema.json).
 *
 * `#page=terminal&project=<place id>` lists that project's terminals;
 * `&terminal=<id>` shows one. The page has no drawer entry: it is reached
 * from the Projects page and the work page's project scope.
 *
 * The console Clawdline Cloud serves says, in one sentence, that terminals are
 * only on the local console and locally paired devices, and draws nothing
 * that could take input.
 */

function currentRoute(): TerminalRoute {
  return terminalRouteFromHash(typeof location === "undefined" ? "" : location.hash)
}

function TerminalPage({ shown }: { shown: boolean }) {
  const [route, setRoute] = useState<TerminalRoute>(currentRoute)
  // The address may name the project by the work page's id or by its folder
  // (the Projects page links by folder); the terminal routes take the id.
  const [place, setPlace] = useState<{ asked: string; id: string; label: string } | "loading" | "failed">("loading")
  const title = useRef<HTMLHeadingElement>(null)
  const hosted = hostedConsole()

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

  useEffect(() => {
    if (!route.project || hosted) return
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
  }, [route.project, hosted])

  useEffect(() => {
    if (shown && !route.terminal) title.current?.focus({ preventScroll: true })
  }, [shown, route.terminal])

  const known = typeof place === "object" && place.asked === route.project ? place : null
  const project = known?.id ?? ""
  const name = known?.label || project || route.project
  const backToWork = () => {
    try {
      history.replaceState(history.state, "", workPageHash(project))
    } catch {
      // refusal-ok: leaving the page still works; only the address is not rewritten.
    }
    requestPage({ page: "work", hash: false })
  }

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
        <div className="terminal-wrap">
          <h1 id="terminal-title" ref={title} tabIndex={-1}>{nextWord("terminalEntry")}</h1>
          <p className="terminal-note" role="note">{nextWord("terminalRefusalCloudNotSupported")}</p>
        </div>
      ) : route.terminal ? (
        <>
          <h1 id="terminal-title" className="terminal-sr">
            {nextWord("terminalEntryFor", { project: name })}
          </h1>
          <TerminalView id={route.terminal} shown={shown} onBack={() => openTerminalPage(project || route.project)} />
        </>
      ) : (
        <div className="terminal-wrap">
          <header className="board-head terminal-page-head">
            <h1 id="terminal-title" ref={title} tabIndex={-1}>{nextWord("terminalEntry")}</h1>
            <button className="board-button" type="button" onClick={backToWork}>{nextWord("terminalBack")}</button>
          </header>
          {!route.project
            ? <p className="terminal-note" role="note">{nextWord("terminalNoProject")}</p>
            : place === "failed"
              ? <p className="terminal-note" role="alert">{nextWord("terminalListFailed", { why: nextWord("terminalRefusalUnreachable") })}</p>
              : !known
                ? <p className="terminal-note">{nextWord("terminalListLoading")}</p>
                : !project
                  ? <p className="terminal-note" role="alert">{nextWord("terminalProjectUnknown")}</p>
                  : <TerminalProjectList project={project} label={name} shown={shown} />}
        </div>
      )}
    </section>
  )
}

export const page: PageModule = { id: "terminal", drawer: false, Component: TerminalPage }
