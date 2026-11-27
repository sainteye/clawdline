import { useMemo, useSyncExternalStore } from "react"
import type * as W from "@clawdline/contract"
import { botIdentity } from "./view-state.js"
import type { SourceClient } from "./client.js"

/**
 * Another surface (the Sessions list's Bot dispatch frame) asks the Bots host
 * to open one Bot's chat by its stable key. The host owns the window, so the
 * request travels as a document event, like the other overlay requests.
 */
export const OPEN_BOT = "clawdline:open-bot"
export type OpenBotRequest = { key: W.ProjectAssistantBotKey; opener?: HTMLElement | null }

/** The Bot a key opens: one that this client currently reads and that is not hidden. Never a name match. */
export function openableBot(items: readonly W.ProjectAssistantBotSummary[], key: W.ProjectAssistantBotKey) {
  const id = botIdentity(key)
  return items.find(item => botIdentity(item.bot.key) === id && !item.bot.hidden)
}

// The host publishes what its sources last read, so a caller can tell an
// openable Bot from a deleted, hidden or unreadable one before offering a press.
let directory: ReadonlySet<string> = new Set()
const listeners = new Set<() => void>()
export function publishBotDirectory(items: readonly W.ProjectAssistantBotSummary[], sources?: readonly SourceClient[],
  states: Readonly<Record<string, BotDirectoryState>> = {}): void {
  publishBots(items, states)
  if (sources) publishSources(sources)
  const next = new Set(items.filter(item => !item.bot.hidden).map(item => botIdentity(item.bot.key)))
  if (next.size === directory.size && [...next].every(id => directory.has(id))) return
  directory = next
  for (const listener of listeners) listener()
}
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener) } }
const snapshot = () => directory

export function useBotOpenable(key: W.ProjectAssistantBotKey | undefined): boolean {
  const current = useSyncExternalStore(subscribe, snapshot, snapshot)
  return !!key && current.has(botIdentity(key))
}

export function requestOpenBot(request: OpenBotRequest): void {
  window.dispatchEvent(new CustomEvent<OpenBotRequest>(OPEN_BOT, { detail: request }))
}

// The same reading with each Bot's name and avatar, for a surface outside the
// Bots window that names a Bot by its ID (the Board's Bot work section).
export type BotDirectoryState = "loading" | "ready" | "unavailable"
let botDirectory: {
  bots: readonly W.ProjectAssistantBot[]
  states: Readonly<Record<string, BotDirectoryState>>
} = {bots: [], states: {}}
let botsMark = ""
const botListeners = new Set<() => void>()
function publishBots(items: readonly W.ProjectAssistantBotSummary[], states: Readonly<Record<string, BotDirectoryState>>): void {
  const mark = JSON.stringify([states, items.map(item => [botIdentity(item.bot.key), item.bot.name, item.bot.appearance, !!item.bot.hidden])])
  if (mark === botsMark) return
  botsMark = mark
  botDirectory = {bots: items.map(item => item.bot), states}
  for (const listener of botListeners) listener()
}
const subscribeBots = (listener: () => void) => { botListeners.add(listener); return () => { botListeners.delete(listener) } }
const botsSnapshot = () => botDirectory

/** The host's last roster, including hidden Bots. A missing name is conclusive only after a complete read. */
export function useBotDirectory(machineID: string | null): {bots: readonly W.ProjectAssistantBot[]; state: BotDirectoryState} {
  const {bots, states} = useSyncExternalStore(subscribeBots, botsSnapshot, botsSnapshot)
  return useMemo(() => {
    if (machineID !== null) return {bots: bots.filter(bot => bot.key.machine_id === machineID), state: states[machineID] ?? "loading"}
    const current = Object.values(states)
    const state = !current.length || current.includes("loading") ? "loading"
      : current.includes("unavailable") ? "unavailable" : "ready"
    return {bots, state}
  }, [bots, states, machineID])
}

// The host's machine sources, so a surface outside the Bots window (the Board's
// Bot work section) reads a Bot's machine the way its chat does: this daemon,
// or the machine a Cloud page shows, through that machine's own source.
let sources: readonly SourceClient[] = []
const sourceListeners = new Set<() => void>()
function publishSources(next: readonly SourceClient[]): void {
  if (next.length === sources.length && next.every((source, index) => source === sources[index])) return
  sources = next
  for (const listener of sourceListeners) listener()
}
const subscribeSources = (listener: () => void) => { sourceListeners.add(listener); return () => { sourceListeners.delete(listener) } }
const sourcesSnapshot = () => sources

/** The host's current source for one machine, or undefined until the host has one. */
export function useBotSource(machineID: string | null): SourceClient | undefined {
  const current = useSyncExternalStore(subscribeSources, sourcesSnapshot, sourcesSnapshot)
  return machineID === null ? undefined : current.find(source => source.machineID === machineID)
}
