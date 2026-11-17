/**
 * What a voice copilot call has cost, from the `usage` every `response.done`
 * carries, priced per 1M tokens as OpenAI's pricing page listed them on
 * 2026-11-12 (developers.openai.com/api/docs/pricing). Whisper captions are
 * billed per minute of audio and are not in `usage`, so they are not counted
 * here; the sheet says so.
 *
 * Nothing is imported here, so `node --test` loads it as it is.
 */

export interface Rates {
  textIn: number
  textCached: number
  textOut: number
  audioIn: number
  audioCached: number
  audioOut: number
  imageIn: number
  imageCached: number
}

export const RATES: Record<string, Rates> = {
  'gpt-realtime-mini': { textIn: 0.6, textCached: 0.06, textOut: 2.4, audioIn: 10, audioCached: 0.3, audioOut: 20, imageIn: 0.8, imageCached: 0.08 },
  'gpt-realtime': { textIn: 4, textCached: 0.4, textOut: 16, audioIn: 32, audioCached: 0.4, audioOut: 64, imageIn: 5, imageCached: 0.5 },
}

export interface CallCost {
  parts?: Record<string, number>
  cachedTokens?: number
  model: string
  responses: number
  inputTokens: number
  outputTokens: number
  audioOutTokens: number
  usd: number
  /** False when a response's model has no rate here: the dollars are then a floor. */
  priced: boolean
  /** A GPT-Live call, billed by the second rather than per response (`copilot-live.ts`). */
  live?: { seconds: number; billedSeconds: number; backendUSD: number }
}

export function emptyCost(model: string): CallCost {
  return { model, responses: 0, inputTokens: 0, outputTokens: 0, audioOutTokens: 0, usd: 0, priced: model in RATES }
}

function n(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) && v > 0 ? v : 0
}

/** The dollars one `response.done` usage block stands for. */
export function usageUSD(usage: unknown, rates: Rates): number {
  const u = (usage ?? {}) as Record<string, any>
  const inD = (u.input_token_details ?? {}) as Record<string, any>
  const cached = (inD.cached_tokens_details ?? {}) as Record<string, any>
  const outD = (u.output_token_details ?? {}) as Record<string, any>
  const textCached = n(cached.text_tokens), audioCached = n(cached.audio_tokens), imageCached = n(cached.image_tokens)
  const textIn = Math.max(0, n(inD.text_tokens) - textCached)
  const audioIn = Math.max(0, n(inD.audio_tokens) - audioCached)
  const imageIn = Math.max(0, n(inD.image_tokens) - imageCached)
  const perToken =
    textIn * rates.textIn + textCached * rates.textCached +
    audioIn * rates.audioIn + audioCached * rates.audioCached +
    imageIn * rates.imageIn + imageCached * rates.imageCached +
    n(outD.text_tokens) * rates.textOut + n(outD.audio_tokens) * rates.audioOut
  return perToken / 1_000_000
}

/** Costs by source, so an input-heavy call is distinguishable from long speech. */
export function usageParts(usage: unknown, rates: Rates): Record<string, number> {
 const u = (usage ?? {}) as Record<string, any>, d=u.input_token_details??{}, c=d.cached_tokens_details??{}, o=u.output_token_details??{}
 return {
  text_input: Math.max(0,n(d.text_tokens)-n(c.text_tokens))*rates.textIn/1e6,
  cached_input: (n(c.text_tokens)*rates.textCached+n(c.audio_tokens)*rates.audioCached+n(c.image_tokens)*rates.imageCached)/1e6,
  text_output: n(o.text_tokens)*rates.textOut/1e6,
  audio_input: Math.max(0,n(d.audio_tokens)-n(c.audio_tokens))*rates.audioIn/1e6,
  audio_output: n(o.audio_tokens)*rates.audioOut/1e6,
  image_input: Math.max(0,n(d.image_tokens)-n(c.image_tokens))*rates.imageIn/1e6,
 }
}

/** The call's running total after one more `response.done`. */
export function addUsage(total: CallCost, usage: unknown): CallCost {
  if (!usage || typeof usage !== 'object') return total
  const u = usage as Record<string, any>
  const rates = RATES[total.model]
  return {
    ...total,
    responses: total.responses + 1,
    cachedTokens: (total.cachedTokens??0)+n(u.input_token_details?.cached_tokens),
    parts: Object.fromEntries(Object.entries(rates?usageParts(u,rates):{}).map(([k,v])=>[k,(total.parts?.[k]??0)+v])),
    inputTokens: total.inputTokens + n(u.input_tokens),
    outputTokens: total.outputTokens + n(u.output_tokens),
    audioOutTokens: total.audioOutTokens + n(u.output_token_details?.audio_tokens),
    usd: total.usd + (rates ? usageUSD(u, rates) : 0),
    priced: total.priced && !!rates && n(u.input_token_details?.text_tokens)+n(u.input_token_details?.audio_tokens)+n(u.input_token_details?.image_tokens)>=n(u.input_tokens) && n(u.output_token_details?.text_tokens)+n(u.output_token_details?.audio_tokens)>=n(u.output_tokens),
  }
}

/** US dollars as a person reads them: a call is usually cents. */
/** A call's length as a person reads it: 0:42, 3:05, 1:02:10. */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h ? `${h}:${pad(m)}:${pad(r)}` : `${m}:${pad(r)}`
}

export function formatUSD(usd: number): string {
  if (usd < 0.01) return `US$${usd.toFixed(4)}`
  return `US$${usd.toFixed(3)}`
}


/** GPT-4.1 mini standard text rates, official model page checked 2026-11-16. */
export function questionAnalysisUSD(model: unknown, usage: unknown): number | undefined {
  if (model !== 'gpt-4.1-mini' || !usage || typeof usage !== 'object') return undefined
  const u = usage as {prompt_tokens?:unknown;completion_tokens?:unknown;prompt_tokens_details?:{cached_tokens?:unknown}}
  if (typeof u.prompt_tokens !== 'number' || typeof u.completion_tokens !== 'number' || u.prompt_tokens + u.completion_tokens <= 0) return undefined
  const cached = Math.min(n(u.prompt_tokens), n(u.prompt_tokens_details?.cached_tokens))
  return ((n(u.prompt_tokens)-cached)*0.4 + cached*0.1 + n(u.completion_tokens)*1.6)/1_000_000
}
