import type { TerminalFrame } from "@clawdline/contract"

export type TerminalDelta = { v: 1; type: "terminal_frame_delta"; terminal_id: string; connection: string;
  frame_seq: number; base_seq: number; base_rev: string; captured_at: number; rev: string; at: number;
  cols: number; rows: number; dead: boolean; cursor: TerminalFrame["cursor"]; modes: TerminalFrame["modes"];
  changed_rows: Array<{ row: number; line: string }>; screen_hash: string }

const bad = () => new Error("terminal_delta_mismatch")
const encoder = new TextEncoder()

export async function terminalScreenHash(lines: string[]): Promise<string> {
  if (lines.length > 0xffffffff) throw bad()
  const chunks: Uint8Array[] = []
  let length = 4
  const count = new Uint8Array(4)
  new DataView(count.buffer).setUint32(0, lines.length)
  chunks.push(count)
  for (const line of lines) {
    const bytes = encoder.encode(line)
    if (bytes.length > 0xffffffff) throw bad()
    const size = new Uint8Array(4)
    new DataView(size.buffer).setUint32(0, bytes.length)
    chunks.push(size, bytes)
    length += 4 + bytes.length
  }
  const input = new Uint8Array(length)
  let offset = 0
  for (const chunk of chunks) { input.set(chunk, offset); offset += chunk.length }
  return [...new Uint8Array(await crypto.subtle.digest("SHA-256", input))]
    .map((byte) => byte.toString(16).padStart(2, "0")).join("")
}

export async function reconstructTerminalDelta(base: TerminalFrame, baseSeq: number, delta: TerminalDelta,
  connection: string, terminal: string, now = Date.now() / 1000): Promise<TerminalFrame> {
  if (delta.v !== 1 || delta.type !== "terminal_frame_delta" || delta.connection !== connection ||
    delta.terminal_id !== terminal || !Number.isSafeInteger(delta.frame_seq) || delta.frame_seq !== baseSeq + 1 ||
    delta.base_seq !== baseSeq || delta.base_rev !== base.rev ||
    !Number.isSafeInteger(delta.cols) || delta.cols <= 0 || !Number.isSafeInteger(delta.rows) || delta.rows <= 0 ||
    delta.cols !== base.cols || delta.rows !== base.rows || delta.modes?.alt !== base.modes.alt ||
    typeof delta.dead !== "boolean" || typeof delta.rev !== "string" ||
    !Number.isFinite(delta.at) || !Number.isFinite(delta.captured_at) || delta.at !== delta.captured_at ||
    Math.abs(now - delta.captured_at) > 6 || !Array.isArray(delta.changed_rows) ||
    !/^[0-9a-f]{64}$/.test(delta.screen_hash)) throw bad()
  const lines = base.lines.slice()
  let previous = -1
  for (const change of delta.changed_rows) {
    if (!Number.isSafeInteger(change.row) || change.row <= previous || change.row >= delta.rows ||
      typeof change.line !== "string") throw bad()
    lines[change.row] = change.line
    previous = change.row
  }
  const frame: TerminalFrame = { rev: delta.rev, at: delta.at, cols: delta.cols, rows: delta.rows,
    dead: delta.dead, cursor: delta.cursor, modes: delta.modes, lines }
  if (!delta.cursor || !Number.isSafeInteger(delta.cursor.x) || !Number.isSafeInteger(delta.cursor.y) ||
    typeof delta.cursor.visible !== "boolean" || !delta.modes ||
    typeof delta.modes.app_cursor !== "boolean" || typeof delta.modes.app_keypad !== "boolean" ||
    typeof delta.modes.mouse_sgr !== "boolean" || typeof delta.modes.alt !== "boolean" ||
    !["none", "standard", "button", "any"].includes(delta.modes.mouse) ||
    await terminalScreenHash(lines) !== delta.screen_hash) throw bad()
  return frame
}
