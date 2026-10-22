import type { SessionRow } from "@clawdline/contract"
import type { StartAnswer, StartPlaceRow } from "../legacy/start-bridge.js"

/** The start reply proves the pane exists; the inventory will fill in its state. */
export function startedRow(answer: StartAnswer, place: StartPlaceRow | null): SessionRow | null {
  if (!answer.id || !["iterm", "tmux", "owned"].includes(answer.backend ?? "") ||
    !["claude", "codex", "antigravity"].includes(answer.assistant ?? "")) return null
  return {
    id: answer.id,
    backend: answer.backend as SessionRow["backend"],
    assistant: answer.assistant as SessionRow["assistant"],
    cwd: answer.cwd ?? place?.path,
    label: place?.label,
    isClaude: answer.assistant === "claude",
    evidence: "none",
    identity: "no_record",
    state: "unknown",
    work_state: "unknown",
    closeability: {
      activity_generation: 0, attestation_id: null, mover: null,
      obligation_generation: 0, observed_at: 0, provenance: [], reasons: [],
      session_generation: 0, source: { freshness: "unknown", max_age_seconds: 0,
        observed_at: 0, provenance: "start_reply" }, state: "unknown", version: "",
    },
  }
}
