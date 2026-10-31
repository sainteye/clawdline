import type { PromptSuggestion } from "@clawdline/contract"

/** Native versions order initial reads and pushed rows without a client clock. */
export class PromptSuggestionState {
  private version = -1
  private suppressed = ""
  private waitingForClear = false
  private lastID = ""
  private held: PromptSuggestion | null = null
  private readonly conversation: string
  constructor(conversation: string) { this.conversation = conversation }
  read(): PromptSuggestion | null { return this.held }
  pause(): void { this.held = null }
  dismiss(): void {
    if (this.lastID) this.suppressed = this.lastID
    else this.waitingForClear = true
    this.held = null
  }
  observe(value: PromptSuggestion | null | undefined): void {
    if (!value || value.source !== "claude-terminal" || value.conversation_id !== this.conversation ||
      !Number.isSafeInteger(value.observed_at) || value.observed_at <= 0 || value.observed_at < this.version) return
    if (value.state !== "none" && value.state !== "available") return
    this.version = value.observed_at
    if (value.state === "none") { this.held = null; this.waitingForClear = false; return }
    if (value.state === "available" && typeof value.id === "string" && !!value.id && typeof value.text === "string" && !!value.text) {
      this.lastID = value.id
      if (!this.waitingForClear && value.id !== this.suppressed) this.held = value
    }
  }
}

