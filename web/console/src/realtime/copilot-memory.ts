/**
 * What the voice copilot remembers between calls on this browser.
 *
 * A Realtime call starts with an empty conversation, so every call opened
 * with the same introduction and knew nothing of the last one. The words each
 * side said (the person's transcription, the copilot's spoken transcript) are
 * kept here and handed to the next call's instructions. Tool results are not
 * kept: they are stale by the next call, and the copilot reads them again.
 *
 * Bounded three ways so it cannot grow without end: the newest
 * MEMORY_TURNS turns, MEMORY_TURN_CHARS each, none older than
 * MEMORY_MAX_AGE_S. Nothing is imported here, so `node --test` loads it as
 * it is.
 */

export interface MemoryTurn {
  role: 'user' | 'assistant'
  text: string
  /** Unix seconds. */
  at: number
}

export const MEMORY_TURNS = 24
export const MEMORY_TURN_CHARS = 300
export const MEMORY_MAX_AGE_S = 3 * 24 * 60 * 60

function clipTurn(text: string): string {
  const t = text.replace(/\s+/g, ' ').trim()
  return t.length > MEMORY_TURN_CHARS ? t.slice(0, MEMORY_TURN_CHARS) + '…' : t
}

/** The turns still worth remembering at `now`, oldest first. */
export function current(turns: readonly MemoryTurn[], now: number): MemoryTurn[] {
  return turns.filter((t) => now - t.at <= MEMORY_MAX_AGE_S && t.at <= now + 60).slice(-MEMORY_TURNS)
}

export function remember(turns: readonly MemoryTurn[], role: MemoryTurn['role'], text: string, now: number): MemoryTurn[] {
  const words = clipTurn(text)
  if (!words) return current(turns, now)
  return current([...turns, { role, text: words, at: now }], now)
}

/** Whatever a stored value is, the turns in it that are well formed. */
export function parseMemory(raw: string | null): MemoryTurn[] {
  if (!raw) return []
  try {
    const value = JSON.parse(raw) as unknown
    if (!Array.isArray(value)) return []
    return value.filter((t): t is MemoryTurn => !!t && typeof t === 'object' &&
      ((t as MemoryTurn).role === 'user' || (t as MemoryTurn).role === 'assistant') &&
      typeof (t as MemoryTurn).text === 'string' && typeof (t as MemoryTurn).at === 'number')
  } catch {
    // refusal-ok: an unreadable stored memory is no memory; the call still starts.
    return []
  }
}

function ago(seconds: number): string {
  if (seconds < 90) return '剛剛'
  const minutes = Math.round(seconds / 60)
  if (minutes < 90) return `${minutes} 分鐘前`
  const hours = Math.round(minutes / 60)
  if (hours < 36) return `${hours} 小時前`
  return `${Math.round(hours / 24)} 天前`
}

/** The instructions' section that tells the model what was said before, or "". */
export function memorySection(turns: readonly MemoryTurn[], now: number): string {
  const kept = current(turns, now)
  if (!kept.length) return ''
  const lines = kept.map((t) => `（${ago(now - t.at)}）${t.role === 'user' ? '使用者' : '你'}：${t.text}`)
  return [
    '【之前的通話】',
    '以下是你和使用者最近幾次通話說過的話，由舊到新。這是背景，不是新的問題：不要重複你已經說過的介紹，使用者提過的事可以直接接續；Session 的狀態可能已經變了，需要時重新呼叫工具查。',
    ...lines,
  ].join('\n')
}
