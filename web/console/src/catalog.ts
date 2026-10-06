import english from "../public/catalogs/en.json" with { type: "json" }
import baseline from "../public/catalogs/baseline-keys.json" with { type: "json" }
import { T, applyStrings } from "./legacy/js/core/i18n.js"

export const CATALOG_TAGS = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"] as const
export type CatalogTag = typeof CATALOG_TAGS[number]
export const DEFAULT_TAG: CatalogTag = "en"
export const UI_LANGUAGE_KEY = "ui_language"

type Catalog = Record<string, string>
const fallback: Catalog = english
let active: Catalog = fallback
let boot: Promise<CatalogTag> | null = null
let missing = new Set<string>()

/** Resolve one BCP 47 tag; script wins over region for Chinese. */
export function resolveTag(input: string): CatalogTag | null {
  const tag = input.trim().replace(/_/g, "-").toLowerCase()
  if (!/^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$/u.test(tag)) return null
  const parts = tag.split("-")
  const base = parts[0]
  if (base === "zh") {
    const script = parts[1]?.match(/^[a-z]{4}$/u)?.[0]
    if (script && script !== "hant" && script !== "hans") return null
    if (parts.includes("hant")) return "zh-Hant"
    if (parts.includes("hans")) return "zh-Hans"
    if (parts.includes("tw") || parts.includes("hk") || parts.includes("mo")) return "zh-Hant"
    if (parts.includes("cn") || parts.includes("sg")) return "zh-Hans"
    return null
  }
  if (base === "pt") return "pt-BR"
  if (base === "en" || base === "ja" || base === "ko" || base === "es" || base === "fr" || base === "de") return base
  return null
}

export function browserPreference(): string {
  try {
    const saved = localStorage.getItem(UI_LANGUAGE_KEY) || "auto"
    return resolveTag(saved) ?? saved
  } catch { return "auto" }
}

export function browserLanguages(): readonly string[] {
  if (typeof navigator === "undefined") return []
  return [...(navigator.languages ?? []), navigator.language].filter(Boolean)
}

export function chooseTag(preference: string, languages: readonly string[]): CatalogTag {
  if (preference !== "auto" && preference !== "") return resolveTag(preference) ?? DEFAULT_TAG
  for (const language of languages) {
    const resolved = resolveTag(language)
    if (resolved) return resolved
  }
  return DEFAULT_TAG
}

export function saveBrowserPreference(value: string): boolean {
  if (value !== "auto" && !CATALOG_TAGS.includes(value as CatalogTag)) return false
  try { localStorage.setItem(UI_LANGUAGE_KEY, value); return true } catch { return false }
}

const holes = (value: string): string => [...value.matchAll(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g)].map((match) => match[1]).sort().join(",")

/** Validate every present value; only post-baseline secondary copy may be absent. */
export function validCatalog(value: unknown, requested: CatalogTag, baselineKeys: readonly string[] = baseline.keys): value is Catalog {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false
  const candidate = value as Record<string, unknown>
  // A daemon English fallback is activated from the bundled reference, not trusted as locale data.
  if (candidate.lang !== requested) return false
  if (candidate.dir !== "ltr") return false
  const strict = requested === "en" || requested === "zh-Hant"
  const required = strict ? Object.keys(fallback) : baselineKeys
  for (const key of required) if (!Object.hasOwn(candidate, key)) return false
  for (const key of Object.keys(candidate)) {
    if (!Object.hasOwn(fallback, key)) return false
    const text = candidate[key]
    if (typeof text !== "string" || !text.trim()) return false
    const forms = text.split("\x1f")
    const referenceForms = fallback[key].split("\x1f")
    if (forms.length !== referenceForms.length || forms.some((form) => !form.trim())) return false
    if (key !== "lang" && key !== "dir" && forms.some((form, index) => holes(form) !== holes(referenceForms[index]))) return false
    if (/<\/?[A-Za-z][^>]*>/u.test(text) || /[\u0000-\u0008\u000B\u000C\u000E-\u001E]/u.test(text)) return false
  }
  return true
}

export function activateCatalog(candidate: unknown, requested: CatalogTag, baselineKeys: readonly string[] = baseline.keys): CatalogTag {
  const valid = validCatalog(candidate, requested, baselineKeys)
  const selected = valid ? candidate : fallback
  missing = new Set(valid && selected.lang !== "en" ? Object.keys(fallback).filter((key) => !Object.hasOwn(selected, key)) : [])
  active = valid ? { ...fallback, ...selected } : fallback
  const legacy: Record<string, string> = { lang: active.lang, dir: active.dir }
  for (const key of Object.keys(T)) legacy[key] = active[`legacy.${key}`]
  applyStrings(legacy)
  document.documentElement.lang = active.lang
  document.documentElement.dir = active.dir
  document.documentElement.setAttribute?.("data-i18n-missing", String(missing.size))
  return active.lang as CatalogTag
}

export function catalogWord(domain: string, key: string): string {
  const id = `${domain}.${key}`
  return active[id] ?? fallback[id] ?? id
}

export function catalogFormat(domain: string, key: string, values: readonly unknown[]): string {
  return catalogWord(domain, key).replace(/\{arg([0-9]+)\}/gu, (_, index: string) => String(values[Number(index)]))
}

/** Keep module-level lookup tables live until the selected catalog has loaded. */
export function localizedLiteralMap<K extends string>(ids: Record<K, string>): Record<K, string> {
  return new Proxy(ids, {
    get(target, key, receiver) {
      const id = Reflect.get(target, key, receiver)
      return typeof id === "string" && typeof key === "string" && Object.hasOwn(target, key)
        ? catalogWord("literal", id) : id
    },
  })
}

export function localizedLiteralList(ids: readonly string[]): readonly string[] {
  return new Proxy(ids, {
    get(target, key, receiver) {
      const id = Reflect.get(target, key, receiver)
      return typeof key === "string" && /^(0|[1-9][0-9]*)$/u.test(key) && typeof id === "string"
        ? catalogWord("literal", id) : id
    },
  })
}

export function currentCatalogTag(): CatalogTag {
  return active.lang as CatalogTag
}

export function missingCatalogKeys(): readonly string[] {
  return [...missing]
}

export function catalogWordLanguage(domain: string, key: string): CatalogTag {
  return missing.has(`${domain}.${key}`) ? "en" : currentCatalogTag()
}

export function resetCatalogForTest(): void {
  active = fallback
  missing.clear()
  boot = null
}

async function fetchCatalog(tag: CatalogTag, url: string, get: typeof fetch): Promise<Catalog> {
  const response = await get(url)
  if (!response.ok) throw new Error(`catalog ${tag}: ${response.status}`)
  return await response.json() as Catalog
}

/** Both transports call this while html.booting still hides their first paint. */
export function bootCatalog(url: (tag: CatalogTag) => string, get: typeof fetch = fetch): Promise<CatalogTag> {
  if (boot) return boot
  boot = (async () => {
    const tag = chooseTag(browserPreference(), browserLanguages())
    if (tag === "en") return activateCatalog(fallback, "en")
    try { return activateCatalog(await fetchCatalog(tag, url(tag), get), tag) }
    catch { return activateCatalog(fallback, "en") }
  })()
  return boot
}

export function bootLocalCatalog(get: typeof fetch = fetch): Promise<CatalogTag> {
  return bootCatalog((tag) => `/v1/strings?lang=${encodeURIComponent(tag)}`, get)
}
