// Tests name copy by what a person reads, not by its catalog hash. Copy moved out of the
// components into catalogs, so a source test's `word("關閉")` answers the pattern for the catalog
// call whose Taiwan Traditional Chinese value is 關閉, and refuses a phrase no catalog key
// translates to; a test of rendered words calls `withCatalog("zh-Hant")` to read them as a person
// with that language selected would.
import { readFileSync } from "node:fs"
import { afterEach, beforeEach } from "node:test"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { activateCatalog, resetCatalogForTest, type CatalogTag } from "./catalog.ts"

type Catalog = Record<string, string>

const read = (tag: string): Catalog => JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8"))
const zh = read("zh-Hant")
const en = read("en")

const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&")

/** The ids whose zh-Hant value is `text`, each also carried in English. */
export function catalogIds(text: string): string[] {
  const ids = Object.keys(zh).filter((id) => zh[id] === text && typeof en[id] === "string" && en[id] !== "")
  if (ids.length === 0) throw new Error(`no catalog key reads ${JSON.stringify(text)} in zh-Hant and has an English value`)
  return ids
}

/** A pattern for the start of a `catalogWord`, `catalogFormat` or `catalogLabel` call naming a key that reads `text`. */
export function word(text: string): string {
  const calls = catalogIds(text).map((id) => {
    const dot = id.indexOf(".")
    return `${escape(id.slice(0, dot))}", "${escape(id.slice(dot + 1))}"`
  })
  return `catalog(?:Word|Format|Label)\\("(?:${calls.join("|")})`
}

/** A pattern for the bare key of an id that reads `text`, as a lookup table names it. */
export function key(text: string): string {
  return `(?:${catalogIds(text).map((id) => escape(id.slice(id.indexOf(".") + 1))).join("|")})`
}

/** A pattern for a whole `catalogWord(...)` call, closing parenthesis included. */
export function wordCall(text: string): string {
  return `${word(text)}\\)`
}

/**
 * A pattern for copy a source must not show, however it is spelled there: the words written in
 * place, or the quoted key (or legacy `T.key`) of any catalog entry whose zh-Hant or English value
 * says them. A bare `doesNotMatch(source, /目前沒有直接待辦/)` passes forever once copy lives in
 * catalogs; this one goes red again the moment a key that says it is named.
 */
export function said(words: string | RegExp): string {
  const text = typeof words === "string" ? escape(words) : words.source
  const says = new RegExp(text, typeof words === "string" ? "u" : words.flags)
  const ids = [...new Set([...Object.keys(zh), ...Object.keys(en)])]
    .filter((id) => id !== "lang" && id !== "dir" && (says.test(zh[id] ?? "") || says.test(en[id] ?? "")))
  const keys = ids.map((id) => escape(id.slice(id.indexOf(".") + 1)))
  const legacy = ids.filter((id) => id.startsWith("legacy.")).map((id) => escape(id.slice("legacy.".length)))
  const ways = [`(?:${text})`]
  if (keys.length) ways.push(`["'\`](?:${keys.join("|")})["'\`]`)
  if (legacy.length) ways.push(`\\.(?:${legacy.join("|")})\\b`)
  return `(?:${ways.join("|")})`
}

/** A regular expression written raw, with `word`/`wordCall` patterns interpolated as they are. */
export function pattern(strings: TemplateStringsArray, ...parts: string[]): RegExp {
  return new RegExp(String.raw(strings, ...parts))
}

function activate(tag: CatalogTag): () => void {
  const stubbed = !("document" in globalThis)
  if (stubbed) Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  const active = activateCatalog(read(tag), tag)
  if (active !== tag) throw new Error(`the ${tag} catalog did not validate; ${active} is active`)
  return () => {
    resetCatalogForTest()
    if (stubbed) Reflect.deleteProperty(globalThis, "document")
  }
}

/** Every test in the calling file runs with `tag`'s catalog active, and the default afterwards. */
export function withCatalog(tag: CatalogTag): void {
  let restore = () => {}
  beforeEach(() => { restore = activate(tag) })
  afterEach(() => restore())
}

/** `run` with `tag`'s catalog active, and the default afterwards. */
export function inCatalog<T>(tag: CatalogTag, run: () => T): T {
  const restore = activate(tag)
  try {
    return run()
  } finally {
    restore()
  }
}
