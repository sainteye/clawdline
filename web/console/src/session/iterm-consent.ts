/** A failed send may offer consent; only the person's explicit confirmation writes it. */
export interface ITermConsentTarget {
  current(): boolean
  read(): Promise<{ iterm_integration?: boolean | null }>
  enable(): Promise<{ iterm_integration?: boolean | null }>
}

export type ITermConsentState = { phase: "closed" } | {
  phase: "asking" | "saving" | "failed" | "saved"
  error?: unknown
}

export class ITermConsent {
  private state: ITermConsentState = { phase: "closed" }
  private target: ITermConsentTarget | null = null
  private checking = false
  private generation = 0
  private listeners = new Set<() => void>()
  snapshot = (): ITermConsentState => this.state
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }
  private set(state: ITermConsentState): void {
    this.state = state
    for (const listener of this.listeners) listener()
  }
  async offer(code: string, target: ITermConsentTarget): Promise<void> {
    if (code !== "backend_unsupported" || this.checking || this.state.phase !== "closed") return
    this.checking = true
    const generation = this.generation
    try {
      const settings = await target.read()
      if (generation === this.generation && target.current() && settings.iterm_integration === false) {
        this.target = target
        this.set({ phase: "asking" })
      }
    } catch { /* The failed message keeps its original refusal when settings cannot be read. */ }
    finally { this.checking = false }
  }
  close = (): void => {
    this.generation++
    this.target = null
    this.set({ phase: "closed" })
  }
  confirm = async (): Promise<void> => {
    const target = this.target
    if (!target || (this.state.phase !== "asking" && this.state.phase !== "failed")) return
    if (!target.current()) { this.close(); return }
    const generation = this.generation
    this.set({ phase: "saving" })
    try {
      const settings = await target.enable()
      if (generation !== this.generation) return
      if (!target.current()) { this.close(); return }
      if (settings.iterm_integration !== true) throw new Error("iTerm2 integration was not enabled")
      this.set({ phase: "saved" })
    } catch (error) {
      if (generation === this.generation) this.set({ phase: "failed", error })
    }
  }
}

export const itermConsent = new ITermConsent()
