import { useCallback, useEffect, useState, type MutableRefObject } from "react"
import type * as W from "@clawdline/contract"
import { authorized, type SourceClient } from "./client"
import { CharterRefusal, charterApiFor, charterProjectKey, charterSummaries, type CharterProject, type CharterSummary } from "./charter-api"
import { CharterPage } from "./CharterPage"
import { charterTemplate } from "./charter-text"
import { charterWord } from "./charter-words"
import type { CharterSettingsRow } from "./CharterSettings"
import type { CharterSettingsTab } from "./BotsSettingsModal"
import type { Words } from "./words"

// The Bots screen's team charters: each machine's listing for the roster rows
// and the settings tab, and which Project's charter fills the centre pane. A
// charter and a chat are never open together.

export interface CharterViewInput {
  sources: readonly SourceClient[]
  current: () => readonly SourceClient[]
  authorization: string
  projectNames: ReadonlyMap<string, string>
  w: Words
  language: string
  narrow: boolean
  manageHistory: boolean
  historyID: MutableRefObject<string>
  writeHistory: (bot: string | null, group: string | null, replace: boolean, charter?: CharterProject | null) => void
  /** A chat or a group opened: the charter gives way. */
  selected: string | null
  groupOpen: boolean
  clear: { setSelected: (id: string | null) => void; setDestination: (value: null) => void; setGroupRoute: (value: string | null) => void
    selectRef: MutableRefObject<((key: W.ProjectAssistantBotKey | null) => void) | undefined>; lastSelected: MutableRefObject<string | null> }
  setSettings: (open: boolean) => void
}

export function useCharterView(input: CharterViewInput) {
  const { sources, current, authorization, projectNames, w, language, narrow, manageHistory, historyID, writeHistory, selected, groupOpen, clear, setSettings } = input
  const closeSettings = () => setSettings(false)
  const [open, setOpen] = useState<CharterProject | null>(null)
  const [charters, setCharters] = useState<ReadonlyMap<string, CharterSummary>>(new Map())
  const [reads, setReads] = useState(0)
  const refresh = useCallback(() => setReads(n => n + 1), [])
  useEffect(() => {
    const control = new AbortController()
    for (const source of sources) {
      const api = charterApiFor(source)
      if (!api) continue
      void authorized(source, current, s => charterSummaries(api, source.machineID, s), control.signal).then(rows => {
        if (!control.signal.aborted) setCharters(old => machineRows(old, source.machineID, rows))
      }, () => {})
    }
    return () => control.abort()
  }, [authorization, current, reads])
  useEffect(() => { if (selected || groupOpen) setOpen(null) }, [selected, groupOpen])
  useEffect(() => {
    if (!manageHistory) return
    const pop = (event: PopStateEvent) => { if (event.state?.paView === historyID.current) setOpen(event.state.paCharter ?? null) }
    window.addEventListener("popstate", pop)
    return () => window.removeEventListener("popstate", pop)
  }, [manageHistory])
  const api = charterApiFor(open ? sources.find(s => s.machineID === open.machine_id) : undefined)
  const openCharter = (project: CharterProject) => {
    clear.lastSelected.current = null
    clear.setDestination(null); clear.setSelected(null); clear.selectRef.current?.(null); setOpen(project)
    if (!manageHistory) return
    const previous = history.state
    clear.setGroupRoute(null)
    writeHistory(null, null, previous?.paView === historyID.current && !!(previous.paBot || previous.paGroup || previous.paCharter), project)
  }
  const close = () => {
    if (manageHistory && history.state?.paView === historyID.current && history.state?.paCharter) history.back()
    else setOpen(null)
  }
  const create = async (row: CharterSettingsRow) => {
    const source = sources.find(s => s.machineID === row.project.machine_id), client = charterApiFor(source)
    if (!source || !client) throw new CharterRefusal("unknown")
    const text = templateText(row.bots)
    await authorized(source, current, s => client.save(row.project, { expected_version: 0, text, reason: charterWord("reasonCreated") }, s), new AbortController().signal)
    refresh(); closeSettings(); openCharter(row.project)
  }
  // Rows come from each machine's listing, which names every Project with Bots.
  const settings: CharterSettingsTab | undefined = sources.some(s => charterApiFor(s)) ? {
    rows: [...charters.values()].map(summary => ({ project: summary.project, name: projectNames.get(charterProjectKey(summary.project)) || w.unnamedProject, bots: summary.names, summary }))
      .sort((a, b) => a.name.localeCompare(b.name, language)),
    onOpen: project => { closeSettings(); openCharter(project) },
    onCreate: create,
  } : undefined
  const page = open && api ? <CharterPage key={charterProjectKey(open)} api={api} project={open} projectName={projectNames.get(charterProjectKey(open)) || w.unnamedProject}
    narrow={narrow} showBack={narrow} onBack={close} onChanged={refresh} w={w} language={language}/> : null
  return { charters, selectedKey: open ? charterProjectKey(open) : null, openCharter, page, settings }
}

function machineRows(old: ReadonlyMap<string, CharterSummary>, machineID: string, rows: readonly CharterSummary[]) {
  const next = new Map([...old].filter(([, row]) => row.project.machine_id !== machineID))
  for (const row of rows) if (row.project.machine_id === machineID) next.set(charterProjectKey(row.project), row)
  return next
}

function templateText(bots: readonly string[]) {
  const five = [1, 2, 3, 4, 5]
  return charterTemplate(five.map(n => charterWord(`templateHeading${n}`)), five.map(n => charterWord(`templateStarter${n}`)), bots, charterWord("templateBotSeparator"))
}
