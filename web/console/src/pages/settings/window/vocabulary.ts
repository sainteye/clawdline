/** `nextconfig.MaxVoiceVocabularyTerms` and `MaxVoiceVocabularyRunes`. */
export const VOICE_VOCABULARY_TERMS = 200
export const VOICE_VOCABULARY_RUNES = 64

/** `nextconfig.ValidListEntry`: one line, no surrounding space, at most 64 characters. */
export function validTerm(term: string): boolean {
  return term !== "" && term.trim() === term && [...term].length <= VOICE_VOCABULARY_RUNES &&
    !/[\u0000-\u001f\u007f-\u009f]/.test(term)
}

/**
 * The list the settings file holds, as the daemon will take it back: trimmed,
 * one copy of each term regardless of case, only valid terms, at most 200 —
 * the rule `cleanTerms` applies when transcribing. A hand-edited file with one
 * bad entry would otherwise make every add and remove refused.
 */
export function cleanVocabulary(list: unknown): string[] {
  if (!Array.isArray(list)) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const entry of list) {
    if (typeof entry !== "string") continue
    const term = entry.trim()
    const key = term.toLowerCase()
    if (!validTerm(term) || seen.has(key)) continue
    seen.add(key)
    out.push(term)
    if (out.length === VOICE_VOCABULARY_TERMS) break
  }
  return out
}
