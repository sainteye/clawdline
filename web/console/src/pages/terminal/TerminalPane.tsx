import { useEffect, useRef, useState, type ReactNode } from "react"
import { nextWord } from "../../next-strings.js"
import { workProjectID, type TerminalRoute } from "../../page-route.js"
import { readProjectPlaces, type ProjectPlacePage } from "../work/api.js"
import { watchTerminalHost, type TerminalHost } from "../../cloud/terminal-host.js"
import { CloudProjectReader, cloudTerminalMachine, resolveCloudTerminalProject, type ProjectReadState } from "./cloud-project.js"
import { hostedConsole } from "./api.js"
import { closeTerminalPane, openTerminalPage } from "./navigate.js"
import { TerminalView } from "./TerminalView.js"
import { CloudTerminalPage } from "./CloudTerminalPage.js"
import "./terminal.css"

/**
 * One terminal, in the Session page's second column: beside the terminal
 * list on a desk, a whole screen on a phone (plan v3 §6 F3; the routes are
 * api/v1/terminals.schema.json). It replaced the terminal page, whose
 * per-project list the Session page's terminal list now is.
 *
 * The address names the project by the work page's id or by its folder (the
 * Projects page linked by folder); the terminal routes take the id.
 *
 * The hosted branch uses its dedicated encrypted terminal channels. The local
 * branch keeps its existing fetch/SSE transport. The hosted address carries the
 * Cloud Project id; the channel is asked by the machine-local one, resolved
 * only for a listed row of the terminal host's own machine (cloud-project.ts).
 */
export function TerminalPane({ route, create = false, shown }: {
  route: TerminalRoute
  /** Hosted, with no terminal in the route: open a new one in the project (`openNewTerminal`). */
  create?: boolean
  shown: boolean
}) {
  const [place, setPlace] = useState<{ asked: string; id: string; label: string } | "loading" | "failed">("loading")
  const [again, setAgain] = useState(0)
  const hosted = hostedConsole()
  const [host, setHost] = useState<TerminalHost | null>(null)
  const [cloud, setCloud] = useState<ProjectReadState<ProjectPlacePage>>({ state: "loading" })
  const reader = useRef<CloudProjectReader<ProjectPlacePage> | null>(null)
  const lastMachine = useRef("")
  if (host) lastMachine.current = host.machine
  const machine = host?.machine ?? lastMachine.current
  const targetMachine = cloudTerminalMachine(route.project, machine)

  useEffect(() => (hosted ? watchTerminalHost(setHost) : undefined), [hosted])

  // Hosted: read the Cloud list, retry on a bounded schedule, and read again at
  // once when the terminal host comes back. A new route starts a new reader.
  useEffect(() => {
    if (!hosted || !route.project || !targetMachine) return
    const next = new CloudProjectReader(() => readProjectPlaces(targetMachine), setCloud)
    reader.current = next
    next.start()
    return () => { next.dispose(); if (reader.current === next) reader.current = null }
  }, [hosted, route.project, targetMachine])
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

  const known = typeof place === "object" && place.asked === route.project ? place : null
  // The machine of the last terminal host: while this pane's own Cloud line
  // renews the host is absent, and the Project it was showing is still the
  // same one, so its name (not the raw Cloud id) stays in the title.
  const resolved = hosted && cloud.state === "ready" && targetMachine ? resolveCloudTerminalProject(route.project, cloud.page.places, targetMachine) : null
  const found = resolved?.kind === "found" ? resolved : null
  const project = hosted ? found?.page ?? "" : known?.id ?? ""
  const name = (hosted ? found?.label : known?.label) || project || route.project
  const backButton = (
    <button className="board-button" type="button" onClick={closeTerminalPane}>{nextWord("terminalBackSessions")}</button>
  )
  const note = (body: ReactNode) => <div className="terminal-wrap">
    <header className="board-head terminal-page-head">{backButton}</header>
    {body}
  </div>

  return (
    <section
      className="pane pane-detail terminal-pane terminal-page"
      id="terminal"
      data-view="one"
      aria-labelledby="terminal-title"
    >
      <h1 id="terminal-title" className="terminal-sr">{nextWord("terminalEntryFor", { project: name })}</h1>
      {hosted ? (
        cloud.state === "failed" ? note(<div className="terminal-note-row">
          <p className="terminal-note" role="alert">{cloud.temporary ? nextWord("terminalProjectsReconnecting") : nextWord("terminalProjectsFailedCode", { code: cloud.code })}
            {" "}{cloud.retryInMs !== null ? nextWord("terminalProjectsRetryIn", { seconds: String(Math.round(cloud.retryInMs / 1000)) }) : nextWord("terminalProjectsRetryStopped")}</p>
          <button className="board-button" type="button" onClick={() => reader.current?.retry()}>{nextWord("terminalRetry")}</button></div>)
          : cloud.state === "loading" ? note(<p className="terminal-note">{nextWord("terminalListLoading")}</p>)
            : !host ? note(<p className="terminal-note" role="status">{nextWord("terminalCloudLineReconnecting")}</p>)
              : !found ? note(<p className="terminal-note" role="alert">{nextWord("terminalProjectUnknown")}</p>)
                : <CloudTerminalPage key={route.terminal || "new"} create={create} project={found.page} channelProject={found.local} machine={targetMachine} label={name} id={route.terminal} shown={shown} />
      ) : place === "failed" ? note(
        <div className="terminal-note-row">
          <p className="terminal-note" role="alert">{nextWord("terminalProjectsFailed")}</p>
          <button className="board-button" type="button" onClick={() => setAgain((n) => n + 1)}>{nextWord("terminalRetry")}</button>
        </div>)
        : !known ? note(<p className="terminal-note">{nextWord("terminalListLoading")}</p>)
          : !project ? note(<p className="terminal-note" role="alert">{nextWord("terminalProjectUnknown")}</p>)
            : <TerminalView key={route.terminal} id={route.terminal} shown={shown} label={name}
              onBack={closeTerminalPane}
              onOpenNew={(id) => openTerminalPage(project, id)} />}
    </section>
  )
}
