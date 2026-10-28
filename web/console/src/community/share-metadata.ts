/** Public sharing uses the same preview text in the edge response and SPA navigation. */
export const SHARE_LIMITS = { fetchMs: 3_000, responseBytes: 1_048_576, textChars: 160 } as const
export const SHARE_ORIGIN = "https://app.clawdline.com"
export const SHARE_IMAGE = SHARE_ORIGIN + "/community-share-v1.png"
export type ShareContent = { title: string; body: string; locale?: string }
export type ShareMetadata = { title: string; description: string; canonical: string; image: string; imageAlt: string; locale: string; type: string; robots: string }
const en = {
  cloud: "Clawdline Cloud", community: "Community", questions: "Q&A · Usage questions", issues: "Issues · Problem reports", ideas: "Ideas · Feature proposals", mine: "My discussions", create: "Start a discussion", help: "Frequently asked questions", admin: "Admin area",
  cloudDescription: "Manage your AI agent sessions with Clawdline Cloud.",
  communityDescription: "Ask questions, report issues and share ideas with the Clawdline open-source community.",
  questionsDescription: "Find answers and discuss how to use Clawdline, its settings and supported workflows.",
  issuesDescription: "Report problems, discuss reproduction steps and follow fixes in Clawdline.",
  ideasDescription: "Propose features, discuss use cases and follow ideas from review to release.",
  mineDescription: "Sign in to find the public discussions you started or joined.",
  createDescription: "Start a public question, issue report or feature proposal in the Clawdline community.",
  helpDescription: "Find answers to common questions about using Clawdline.",
  adminDescription: "Sign in with an authorized administrator account to manage Clawdline community discussions.",
  imageAlt: "Clawdline community — questions, issues and ideas",
}
const zh: typeof en = {
  cloud: "Clawdline Cloud", community: "社群討論", questions: "使用問答", issues: "Issues · 問題回報", ideas: "Ideas · 功能提案", mine: "我的討論", create: "發起討論", help: "常見問題", admin: "Admin 專區",
  cloudDescription: "透過 Clawdline Cloud 管理你的 AI Agent 工作階段。",
  communityDescription: "在 Clawdline 開源社群提問、回報問題、分享想法，一起討論更好的使用方式。",
  questionsDescription: "查找答案，一起討論 Clawdline 的操作、設定與支援的工作流程。",
  issuesDescription: "回報 Clawdline 的使用問題、討論重現步驟，並追蹤修正進度。",
  ideasDescription: "提出 Clawdline 功能建議、討論使用情境，追蹤提案從審核到推出的進度。",
  mineDescription: "登入後查看你發起或參與的公開討論。",
  createDescription: "在 Clawdline 社群提出使用問題、回報異常或分享功能建議。",
  helpDescription: "查找 Clawdline 操作與設定的常見問題解答。",
  adminDescription: "使用具管理權限的帳號登入，管理 Clawdline 社群討論。",
  imageAlt: "Clawdline 社群：使用問答、問題回報與功能提案",
}
export function shareChinese(locale: string): boolean { return /^(zh-Hant|zh-TW|zh-HK)(?:-|$)/i.test(locale) }
/** Strip formatting and embedded markup before an excerpt can reach an HTML attribute. */
export function shareText(value: string): string {
  return Array.from(value.replace(/<[^<>]*>/g, " ").replace(/!\[[^\[\]]*\]\([^)]*\)/g, " ").replace(/\[([^\[\]]+)\]\([^)]*\)/g, "$1").replace(/https?:\/\/\S+/g, " ").replace(/[`*_#>~]/g, "").replace(/[\u0000-\u001f\u007f]/g, " ").replace(/\s+/g, " ").trim()).slice(0, SHARE_LIMITS.textChars).join("")
}
export function shareMetadata(address: string, locale = "en", content?: ShareContent): ShareMetadata {
  const url = new URL(address, SHARE_ORIGIN), path = url.pathname.replace(/\/+$/, "") || "/"
  const words = shareChinese(content?.locale ?? locale) ? zh : en
  let title = words.cloud, description = words.cloudDescription, canonical = path, publicPage = false
  if (path === "/community" || path === "/community/issues") { title = words.issues; description = words.issuesDescription; canonical = "/community/issues"; publicPage = true }
  else if (path === "/community/questions") { title = words.questions; description = words.questionsDescription; publicPage = true }
  else if (path === "/community/ideas") { title = words.ideas; description = words.ideasDescription; publicPage = true }
  else if (path === "/community/mine" || (path === "/help/questions" || path.startsWith("/help/questions/"))) { title = words.mine; description = words.mineDescription; canonical = "/community/mine" }
  else if (path === "/community/new" || path === "/help/ask") { title = words.create; description = words.createDescription; canonical = "/community/new" }
  else if (path === "/help" || path.startsWith("/help/faq/")) { title = words.help; description = words.helpDescription; publicPage = path === "/help" }
  else if (path.startsWith("/help/admin")) { title = words.admin; description = words.adminDescription; canonical = "/help/admin" }
  else if (path.startsWith("/community/topics/")) { title = words.community; description = words.communityDescription }
  if (content && (path.startsWith("/community/topics/") || path.startsWith("/help/faq/"))) {
    const cleanTitle = shareText(content.title)
    if (cleanTitle) { title = cleanTitle; description = shareText(content.body) || description; publicPage = true }
  }
  return { title: title === words.cloud ? title : title + " | Clawdline", description, canonical: SHARE_ORIGIN + canonical, image: SHARE_IMAGE, imageAlt: words.imageAlt,
    locale: words === zh ? "zh_TW" : "en_US", type: content && publicPage ? "article" : "website", robots: publicPage ? "index, follow" : "noindex, nofollow" }
}
export function metadataFields(m: ShareMetadata): [string, string, string][] {
  return [["name","description",m.description],["name","robots",m.robots],["property","og:site_name","Clawdline"],["property","og:title",m.title],["property","og:description",m.description],["property","og:url",m.canonical],["property","og:type",m.type],["property","og:locale",m.locale],["property","og:image",m.image],["property","og:image:secure_url",m.image],["property","og:image:type","image/png"],["property","og:image:width","1200"],["property","og:image:height","630"],["property","og:image:alt",m.imageAlt],["name","twitter:card","summary_large_image"],["name","twitter:title",m.title],["name","twitter:description",m.description],["name","twitter:image",m.image],["name","twitter:image:alt",m.imageAlt]]
}
const escape = (s: string) => s.replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[c]!)
export function metadataHTML(m: ShareMetadata): string {
  return `<title>${escape(m.title)}</title><link rel="canonical" href="${escape(m.canonical)}">` + metadataFields(m).map(([key,name,value])=>`<meta ${key}="${name}" content="${escape(value)}">`).join("")
}
export function applyShareMetadata(m: ShareMetadata, doc: Document = document): void {
  doc.title = m.title
  for (const [key,name,value] of metadataFields(m)) {
    const matches = [...doc.head.querySelectorAll<HTMLMetaElement>(`meta[${key}="${name}"]`)]
    const el = matches.shift() ?? doc.createElement("meta")
    for (const extra of matches) extra.remove()
    el.setAttribute(key,name); el.content = value; if (!el.isConnected) doc.head.append(el)
  }
  const links = [...doc.head.querySelectorAll<HTMLLinkElement>('link[rel="canonical"]')], link = links.shift() ?? doc.createElement("link")
  for (const extra of links) extra.remove()
  link.rel = "canonical"; link.href = m.canonical; if (!link.isConnected) doc.head.append(link)
}
