// Functions evaluated inside the page through Runtime.evaluate. Each is
// serialized with Function.prototype.toString, so it must be self-contained.

/* Visible text and the elements a person could press or fill. */
export function readPage(textLimit, elementLimit) {
  const visible = (el) => {
    const r = el.getBoundingClientRect()
    const s = getComputedStyle(el)
    return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none"
  }
  const label = (el) => (el.getAttribute("aria-label") || el.innerText || (el.tagName === "INPUT" && /^(button|submit|reset)$/.test(el.type) ? el.value : "") || el.placeholder ||
    el.getAttribute("title") || el.getAttribute("alt") || el.name || "").replace(/\s+/g, " ").trim().slice(0, 120)
  const sel = "a[href],button,input,select,textarea,summary,[role=button],[role=link],[role=tab],[role=menuitem],[role=checkbox],[contenteditable=true]"
  const elements = []
  for (const el of document.querySelectorAll(sel)) {
    if (elements.length >= elementLimit) break
    if (!visible(el)) continue
    const tag = el.tagName.toLowerCase()
    elements.push({ tag, role: el.getAttribute("role") || undefined, type: el.type || undefined, label: label(el) })
  }
  const text = (document.body ? document.body.innerText : "").replace(/\n{3,}/g, "\n\n")
  return { url: location.href, title: document.title, text: text.slice(0, textLimit), truncated: text.length > textLimit, elements }
}

/* The centre of the best match for a person's description of an element. */
export function locate(target) {
  const want = String(target).replace(/\s+/g, " ").trim().toLowerCase()
  const sel = "a[href],button,input,select,textarea,summary,label,[role=button],[role=link],[role=tab],[role=menuitem],[role=checkbox],[onclick],[contenteditable=true]"
  const label = (el) => (el.getAttribute("aria-label") || el.innerText || (el.tagName === "INPUT" && /^(button|submit|reset)$/.test(el.type) ? el.value : "") || el.placeholder ||
    el.getAttribute("title") || el.getAttribute("alt") || el.name || "").replace(/\s+/g, " ").trim()
  const candidates = []
  for (const el of document.querySelectorAll(sel)) {
    const r = el.getBoundingClientRect()
    if (r.width === 0 || r.height === 0) continue
    const name = label(el)
    const low = name.toLowerCase()
    const score = low === want ? 3 : low.startsWith(want) ? 2 : low.includes(want) ? 1 : 0
    if (score) candidates.push({ el, name, score, len: name.length })
  }
  candidates.sort((a, b) => b.score - a.score || a.len - b.len)
  const hit = candidates[0]
  if (!hit) return { found: false, count: 0 }
  hit.el.scrollIntoView({ block: "center", inline: "center" })
  const r = hit.el.getBoundingClientRect()
  return { found: true, name: hit.name.slice(0, 120), x: r.left + r.width / 2, y: r.top + r.height / 2, count: candidates.length }
}

/* Geometry for a phone's native keyboard. No field text, value, name or label
   crosses this boundary. Shadow-root fields use the same viewport coordinates. */
export function editableRects(limit) {
  const fields = []
  const visit = (root) => {
    for (const el of root.querySelectorAll("*")) {
      if (fields.length >= limit) return
      if (el.shadowRoot) visit(el.shadowRoot)
      if (fields.length >= limit) return
      const tag = el.tagName, type = String(el.type || "text").toLowerCase()
      if (el.disabled || el.readOnly || !(tag === "TEXTAREA" || el.isContentEditable || (tag === "INPUT" && /^(text|password|search|email|url|tel|number)$/.test(type)))) continue
      const r = el.getBoundingClientRect(), style = getComputedStyle(el)
      if (r.width <= 0 || r.height <= 0 || style.display === "none" || style.visibility === "hidden") continue
      fields.push({ x: r.left, y: r.top, width: r.width, height: r.height, kind: tag === "TEXTAREA" || el.isContentEditable ? "multiline" : type })
    }
  }
  visit(document)
  return fields
}

export function focusedEditableKind() {
  let el = document.activeElement
  while (el?.shadowRoot?.activeElement) el = el.shadowRoot.activeElement
  if (!el || el.disabled || el.readOnly) return null
  if (el.tagName === "TEXTAREA" || el.isContentEditable) return "multiline"
  return el.tagName === "INPUT" && /^(text|password|search|email|url|tel|number)$/.test(el.type) ? el.type : null
}
