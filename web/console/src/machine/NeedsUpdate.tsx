import { useSyncExternalStore } from "react"
import type { MachineNeedsUpdate } from "@clawdline/core"
import { nextWord } from "../next-strings.js"
import { needsUpdateWords, sameMachineVersion, type MachineVersion } from "./needs-update-model.js"

/*
 * The current machine's version and route level, from the one health read
 * the connection light already makes every fifteen seconds (App.tsx
 * `useConnectionLight` publishes it here). A second poll would be a second
 * request for the same answer.
 */
let current: MachineVersion = {}
const listeners = new Set<() => void>()

/** Called with each health answer's reading; a reading that did not change wakes nobody. */
export function publishMachineVersion(next: MachineVersion): void {
  if (sameMachineVersion(current, next)) return
  current = next
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

/** `{ version?, apiLevel? }` for the machine this console reads; absent is unknown. */
export function useMachineVersion(): MachineVersion {
  return useSyncExternalStore(subscribe, () => current, () => current)
}

/**
 * 「這台機器的 Clawdline 版本較舊，沒有這個功能。更新後就能使用。」, the
 * machine's version when known, and the way to the Settings page where
 * updating is. It replaces a feature's "could not load", never its empty state.
 */
export function NeedsUpdate({ update, className = "work-note" }: { update: MachineNeedsUpdate | null; className?: string }) {
  const known = useMachineVersion()
  const words = needsUpdateWords(update, known, nextWord)
  return (
    <p className={className} role="status" data-needs-update={update?.code ?? ""} style={{ overflowWrap: "anywhere" }}>
      {words.sentence}
      {words.version ? <> {words.version}</> : null}{" "}
      <a href={words.href}>{words.link}</a>
    </p>
  )
}
