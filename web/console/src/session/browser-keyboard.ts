/** One in-flight native input, with adjacent text coalesced while Cloud is
 * slow. No input is retried after an unknown result, handback or a new field. */
export const BROWSER_INPUT_EDITS = 32
export const BROWSER_INPUT_BYTES = 16384
export type BrowserEdit = { op: "type" | "key"; args: { text?: string; key?: string } }
export type InputReceipt = { ok: boolean; editable?: string | null }

export class BrowserInputQueue {
  private edits: (BrowserEdit & { bytes: number })[] = []
  private bytes = 0
  private epoch = 0
  private stopped = true
  private running = false
  private ready: Promise<boolean> = Promise.resolve(false)
  private send: (edit: BrowserEdit) => Promise<InputReceipt>
  private refuse: (reason: "full" | "unconfirmed") => void
  private changed: (pending: boolean) => void
  private focus: (kind: string | null) => void
  constructor(send: (edit: BrowserEdit) => Promise<InputReceipt>, refuse: (reason: "full" | "unconfirmed") => void, changed: (pending: boolean) => void, focus: (kind: string | null) => void) {
    this.send = send; this.refuse = refuse; this.changed = changed; this.focus = focus
  }
  start(ready: Promise<boolean>) { this.cancel(); this.stopped = false; this.ready = ready }
  cancel() { this.epoch++; this.stopped = true; this.edits = []; this.bytes = 0; this.changed(false) }
  get pending() { return this.running || this.edits.length > 0 }
  add(edit: BrowserEdit): boolean {
    if (this.stopped) return false
    const bytes = new TextEncoder().encode(edit.args.text ?? edit.args.key ?? "").length
    const last = this.edits.at(-1)
    const merge = edit.op === "type" && last?.op === "type"
    if (this.bytes + bytes > BROWSER_INPUT_BYTES || (!merge && this.edits.length >= BROWSER_INPUT_EDITS)) {
      this.cancel(); this.refuse("full"); return false
    }
    if (merge) { last.args.text = (last.args.text ?? "") + (edit.args.text ?? ""); last.bytes += bytes }
    else this.edits.push({ op: edit.op, args: { ...edit.args }, bytes })
    this.bytes += bytes; this.changed(true); void this.drain(); return true
  }
  private async drain() {
    if (this.running || this.stopped) return
    this.running = true
    const epoch = this.epoch
    try {
      if (!await this.ready) {
        if (epoch === this.epoch) { this.cancel(); this.refuse("unconfirmed") }
        return
      }
      while (epoch === this.epoch && !this.stopped && this.edits.length) {
        const edit = this.edits.shift()!; this.bytes -= edit.bytes
        let answer: InputReceipt
        try { answer = await this.send({ op: edit.op, args: edit.args }) } catch { answer = { ok: false } }
        if (epoch !== this.epoch) return
        if (!answer.ok) { this.cancel(); this.refuse("unconfirmed"); return }
        if (answer.editable !== undefined) {
          this.focus(answer.editable)
          if (answer.editable === null) { this.cancel(); return }
        }
      }
    } finally {
      this.running = false
      if (!this.stopped && this.edits.length) void this.drain()
      else this.changed(false)
    }
  }
}
