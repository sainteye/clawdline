const EXACT: Record<string, string> = {
  "Local usage ledger": "本機用量帳本",
  "Project Portfolio": "專案總覽",
  "See where agent work is happening without turning token volume into a score.": "看見 Agent 工作發生在哪裡，而不把 token 用量當成分數。",
  "Back to sessions": "回到 Session 清單",
  "Usage views": "用量檢視",
  Portfolio: "總覽",
  "Recent agent work": "最近的 Agent 工作",
  "Inclusive local date range": "包含起訖日的本機日期範圍",
  From: "從",
  To: "到",
  Timezone: "時區",
  "Refresh portfolio": "重新整理總覽",
  "Generated output is an operational signal, not a productivity score.": "產出量是運作訊號，不是生產力分數。",
  "Portfolio range summary": "總覽期間摘要",
  "Generated output": "產出量",
  "Change unavailable": "目前無法比較變化",
  "Agent work": "Agent 工作",
  "Distinct tasks or measured sessions": "不重複的任務或已量測 Session",
  "Scheduled work": "排程工作",
  "Output coverage": "產出量涵蓋率",
  "Unknown stays unknown": "不明資料仍標示為不明",
  "Ranked by generated output": "依產出量排序",
  Projects: "專案",
  "Project portfolio": "專案總覽",
  Project: "專案",
  Scheduled: "排程",
  "Root / child": "Root／Child",
  "Estimated spending (Claude Code)": "預估費用（Claude Code）",
  Coverage: "涵蓋率",
  Change: "變化",
  Open: "開啟",
  Viewing: "正在查看",
  "Selected Project": "選取的專案",
  "Choose a Project": "選擇一個專案",
  "Select a ranked Project to inspect its measured work.": "選擇排序中的專案，查看它已量測的工作。",
  "Output trend": "產出量趨勢",
  "Assistant & work mix": "助理與工作組成",
  "Root / child work": "Root／Child 工作",
  "Operational clues": "運作線索",
  "Worth a closer look": "值得仔細查看",
  "Explicit schedule identity only": "只計入有明確排程身分的項目",
  "Scheduled Work": "排程工作",
  "Which schedules consume usage, how many days they ran, and whether their output was measurable.": "哪些排程用了用量、執行了幾天，以及產出量能否量測。",
  Schedule: "排程",
  Runs: "執行次數",
  "Active days": "執行天數",
  "Accepted attribution only": "只計入已接受的歸屬",
  Features: "功能",
  "A Feature appears only with one unambiguous accepted attribution head.": "只有歸屬明確且已接受時，功能才會出現在這裡。",
  Feature: "功能",
  Implementation: "實作",
  Review: "複審",
  "Unproved role": "未證實的角色",
  "Limits beside totals": "總數旁的限制",
  "Coverage & export": "涵蓋率與匯出",
  "Lossless JSON": "無損 JSON",
  "Newest first": "最新的在前",
  "Open one item for its accounting facts. Prompts, session ids, and paths are never returned.": "開啟一筆即可查看計量事實；提示、Session ID 與路徑一律不會回傳。",
  "Load more": "載入更多",
  Close: "關閉",
  "Usage detail": "用量明細",
  Unknown: "不明",
  "Unknown Project": "專案不明",
  "Unknown model": "模型不明",
  Root: "Root",
  Child: "Child",
  Unavailable: "無法取得",
  Complete: "完整",
  Partial: "部分",
  "Tokens unknown": "Token 不明",
  "Tokens absent": "沒有 Token 記錄",
  "Cost unknown": "費用不明",
  "Cost absent": "沒有費用記錄",
  "No Project work in this range": "這段期間沒有專案工作",
  "No output buckets in this range.": "這段期間沒有產出量區間。",
  "Assistant and work mix unavailable.": "無法取得助理與工作組成。",
  "No recent work.": "沒有最近的工作。",
  "No explicit schedule identity in this range": "這段期間沒有身分明確的排程",
  "Every scheduled run in this range has explicit schedule identity.": "這段期間每次排程執行都有明確身分。",
  "No Feature reached the acceptance threshold in this range": "這段期間沒有功能達到接受門檻",
  "No accepted Feature attribution in this range": "這段期間沒有已接受的功能歸屬",
  "Operational clue": "運作線索",
  "Inspect the underlying work before acting.": "採取行動前，先查看底層工作。",
  "No sound cross-range clue is available yet. This is not a zero; the necessary comparison may be unavailable.": "目前沒有可靠的跨期間線索。這不代表零；所需的比較資料可能無法取得。",
  "Unknown token rows": "Token 不明的資料列",
  Corrections: "更正",
  Started: "開始時間",
  Assistant: "助理",
  Model: "模型",
  "New input": "新輸入",
  "Cache read": "快取讀取",
  "Cache write": "快取寫入",
  "Measured floor": "已量測下限",
  "Strict total": "嚴格總數",
  "Coverage reasons": "涵蓋率原因",
  None: "無",
  "Unknown token parts": "不明的 Token 部分",
  Cost: "費用",
  "Missing cost": "缺少費用的原因",
  Ended: "結束時間",
  "Source total": "來源總數",
  Reconciliation: "核對結果",
  "Input basis": "輸入計算基礎",
  "An export is already being prepared.": "已經在準備另一份匯出檔。",
  "Export downloaded.": "匯出檔已下載。",
  "Reading the local ledger…": "正在讀取本機用量帳本…",
  "Load more queued…": "已排入載入更多…",
  "Refresh queued…": "已排入重新整理…",
  "Usage Analytics is busy; sessions remain available. Try again shortly.": "用量分析忙碌中；Session 仍可使用，請稍後再試。",
  "This range exceeds the matched-row export limit. Narrow the dates or filters and try again.": "這段期間超過相符資料列的匯出上限。請縮短日期或收窄篩選條件後再試。",
  "Usage could not be read.": "讀不到用量。",
  "choose a closed date range": "請選擇有起訖日的日期範圍",
  "one range exceeds the scan limit": "其中一段期間超過掃描上限",
  "no work in the equal previous range": "等長的前一段期間沒有工作",
  "output coverage is incomplete": "產出量涵蓋不完整",
  "ranges are not comparable": "兩段期間無法比較",
  "top mover": "產出量變化最大",
  "context to output": "上下文與產出比",
  "coverage degradation": "涵蓋率下降",
  "cost concentration": "費用集中",
  "Largest output change": "產出量變化幅度最大",
  "High context-to-output ratio": "上下文與產出的比例偏高",
  "Coverage declined": "涵蓋率下降",
  "Comparable cost is concentrated": "可比較的費用過於集中",
}

const REASONS: Record<string, string> = {
  "partial cost coverage": "費用涵蓋不完整",
  "no claude code usage": "沒有 Claude Code 用量",
  "no cost series": "沒有費用序列",
  "mixed cost series": "費用序列混合",
  "no cost recorded": "沒有費用記錄",
  "no price for model": "此模型沒有價格",
  "source missing": "缺少來源",
  "source unreadable": "無法讀取來源",
  "source unreadable at close": "結束時無法讀取來源",
  "no usage recorded": "沒有用量記錄",
  "session unresolved": "Session 無法確定",
  "legacy ledger unreadable": "無法讀取舊版帳本",
  "project key missing": "缺少專案鍵",
  "schedule identity missing": "缺少排程身分",
  "lineage evidence missing": "缺少工作歸屬證據",
  "no accepted attribution": "沒有已接受的歸屬",
  "no unambiguous accepted head": "沒有明確且已接受的歸屬",
  "scan limit reached": "已達掃描上限",
  complete: "完整",
  partial: "部分資料",
  unavailable: "無法取得",
}

const COST_BASES: Record<string, string> = {
  list_price_estimate: "牌價估算",
  published_rate_estimate: "公開費率估算",
  provider_actual: "供應商實際金額",
  unknown: "依據不明",
}

function reason(value: string): string {
  const key = value.replaceAll("_", " ").toLowerCase()
  return REASONS[key] || value
}

function reasonList(value: string): string {
  return value.split(", ").map(reason).join("、")
}

function compound(value: string): string {
  return value.split(" · ").map(translateUsageText).join(" · ")
}

function freshness(value: string): string {
  if (value === "current") return "目前"
  if (value === "stale") return "已過期"
  if (value === "unknown") return "不明"
  return value
}

/** Translate one complete piece of copy emitted by the copied Usage view. */
export function translateUsageText(value: string): string {
  if (EXACT[value]) return EXACT[value]

  const knownReason = reason(value)
  if (knownReason !== value) return knownReason

  let found = /^(\d+) Projects?$/.exec(value)
  if (found) return `${found[1]} 個專案`
  found = /^(\d+) Features?$/.exec(value)
  if (found) return `${found[1]} 個功能`
  found = /^(\d+) Features? · (\d+) hidden$/.exec(value)
  if (found) return `${found[1]} 個功能 · 隱藏 ${found[2]} 個`
  found = /^(\d+) unknown$/.exec(value)
  if (found) return `${found[1]} 筆不明`
  found = /^Open (.+) details$/.exec(value)
  if (found) return `開啟 ${found[1]} 的詳細資料`
  found = /^(\d+) scheduled runs · (\d+) Unknown output$/.exec(value)
  if (found) return `${found[1]} 次排程執行 · ${found[2]} 次產出量不明`
  found = /^Ledger freshness: (.+)$/.exec(value)
  if (found) return `用量帳本新鮮度：${freshness(found[1])}`
  found = /^Schema (.+)$/.exec(value)
  if (found) return `結構版本 ${found[1]}`
  found = /^Range data through: (.+)$/.exec(value)
  if (found) return `期間資料截至：${found[1] === "none" ? "無" : found[1]}`
  found = /^Observed prices: (.+)$/.exec(value)
  if (found) return `已觀測價格：${found[1] === "none" ? "無" : found[1]}`
  found = /^Preparing (\w+) export…$/.exec(value)
  if (found) return `正在準備 ${found[1].toUpperCase()} 匯出檔…`
  found = /^(\d+) rows have unknown output$/.exec(value)
  if (found) return `${found[1]} 列的產出量不明`
  found = /^(.+) · partial$/.exec(value)
  if (found) return `${found[1]} · 部分資料`
  found = /^(Partial|Unavailable) · (.+) unknown output$/.exec(value)
  if (found) return `${found[1] === "Partial" ? "部分" : "無法取得"} · ${found[2]} 次產出量不明`
  found = /^Unavailable · (.+)$/.exec(value)
  if (found) return `無法取得 · ${reason(found[1])}`
  found = /^(.+?) ([A-Z]{3}) · ([a-z_]+)$/.exec(value)
  if (found && COST_BASES[found[3]]) return `${found[1]} ${found[2]} · ${COST_BASES[found[3]]}`
  found = /^(.+): (.+) generated output$/.exec(value)
  if (found) return `${found[1]}：產出量 ${found[2]}`
  found = /^Lineage (.+) · missing evidence stays Unknown; scheduled work is never guessed as root or child\.$/.exec(value)
  if (found) return `工作歸屬${found[1] === "partial" ? "只有部分資料" : "無法取得"} · 缺少的證據仍標示為不明；排程工作不會被猜成 Root 或 Child。`
  found = /^(.+) generated output across (.+) agent-work runs · (.+)\.$/.exec(value)
  if (found) return `${found[1]} 產出量，來自 ${found[2]} 次 Agent 工作 · ${compound(found[3])}。`
  found = /^(\d+) scheduled runs remain Unknown Schedule because identity is missing\.$/.exec(value)
  if (found) return `${found[1]} 次排程執行因缺少身分，仍歸在「排程不明」。`
  found = /^(\d+) runs remain Unknown Feature\. Proposals, rejections, and conflicting accepted heads never enter a named total\.$/.exec(value)
  if (found) return `${found[1]} 次執行仍歸在「功能不明」。提議、遭拒與互相衝突的已接受歸屬，都不會算進具名總數。`
  found = /^Show the first (\d+)$/.exec(value)
  if (found) return `只顯示前 ${found[1]} 個`
  found = /^Show (\d+) more$/.exec(value)
  if (found) return `再顯示 ${found[1]} 個`
  found = /^Local classifier (.+) v(.+) proposes; the policy accepts at confidence ≥ (.+)\. Only one unambiguous accepted head enters a named total\.$/.exec(value)
  if (found) return `本機分類器 ${found[1]} v${found[2]} 會提出歸屬；政策接受信心值 ≥ ${found[3]} 的結果。只有一個明確且已接受的歸屬會算進具名總數。`
  if (value === "Automatic Feature attribution is not configured. Accepted manual or external assignments appear here.") {
    return "尚未設定自動功能歸屬。已接受的手動或外部指派會顯示在這裡。"
  }
  found = /^(≥?[^ ]+) tokens · partial$/.exec(value)
  if (found) return `${found[1]} token · 部分資料`
  found = /^([^ ]+) tokens$/.exec(value)
  if (found) return `${found[1]} token`
  found = /^(Tokens unknown|Tokens absent) · (.+)$/.exec(value)
  if (found) return `${EXACT[found[1]]} · ${translateUsageText(found[2])}`
  found = /^(≥?[^ ]+) tokens · (Cost|Costs) (.+)$/.exec(value)
  if (found) return `${found[1]} token · 費用 ${found[3].replace(" · mixed series", " · 混合系列").replace(/ · (\d+) cost unknown$/, " · $1 筆費用不明")}`
  found = /^Cost unknown(?: · (.+))?$/.exec(value)
  if (found) return `費用不明${found[1] ? ` · ${reasonList(found[1])}` : ""}`
  found = /^Cost absent$/.exec(value)
  if (found) return "沒有費用記錄"
  found = /^Output (.+)$/.exec(value)
  if (found) return `產出量 ${found[1]}`
  found = /^New input (.+)$/.exec(value)
  if (found) return `新輸入 ${found[1]}`
  found = /^Cache read (.+)$/.exec(value)
  if (found) return `快取讀取 ${found[1]}`
  found = /^Change unavailable · (.+)$/.exec(value)
  if (found) return `目前無法比較變化 · ${EXACT[found[1]] || found[1]}`
  found = /^Coverage · (.+)$/.exec(value)
  if (found) return `涵蓋率 · ${reason(freshness(found[1]))}`
  found = /^(.+) changed by (-?[\d.]+) generated tokens versus the equal previous range\. Inspect the work mix before drawing a conclusion\.$/.exec(value)
  if (found) return `${found[1]} 與等長的前一段期間相比，產出 token 變化了 ${found[2]}。下結論前，請先檢視工作組成。`
  found = /^(.+) read ([\d.]+)x as much new-plus-cached context as it generated\. This can be normal for review or retrieval-heavy work; inspect recent runs\.$/.exec(value)
  if (found) return `${found[1]} 讀取的新增與快取上下文，是其產出的 ${found[2]} 倍。這在複審或需大量擷取的工作中可能正常；請檢視近期執行紀錄。`
  found = /^Complete rows fell from ([\d.]+)% to ([\d.]+)% versus the equal previous range\. Check the named coverage reasons before using totals\.$/.exec(value)
  if (found) return `與等長的前一段期間相比，完整資料列從 ${found[1]}% 降至 ${found[2]}%。使用總數前，請先檢查列出的涵蓋率原因。`
  found = /^(.+) accounts for ([\d.]+)% of Claude Code's comparable (.+) \/ (.+) estimated spending in this range\.$/.exec(value)
  if (found) return `在這段期間，${found[1]} 佔 Claude Code 可比較的 ${found[3]} / ${COST_BASES[found[4]] || found[4]} 預估費用 ${found[2]}%。`
  found = /^Refresh failed\. Showing stale data for (.+); controls currently request (.+)\.$/.exec(value)
  if (found) return `重新整理失敗。現在顯示 ${found[1]} 的舊資料；控制項目前要求 ${found[2]}。`
  found = /^Usage could not be read \((\d+)\)\.$/.exec(value)
  if (found) return `讀不到用量（${found[1]}）。`
  found = /^Partial result: more matching rows exist than this bounded query can read\. Narrow the dates before relying on totals or export\.$/.exec(value)
  if (found) return "只讀到部分結果：相符資料列超過這次查詢的讀取上限。要依賴總數或匯出前，請先縮短日期範圍。"
  found = /^Partial result: (.+)\.$/.exec(value)
  if (found) return `只讀到部分結果：${found[1] === "unknown reason" ? "原因不明" : found[1]}。`

  return value
}

function translated(value: string): string {
  const middle = value.trim()
  if (!middle) return value
  const at = value.indexOf(middle)
  const leading = value.slice(0, at)
  const trailing = value.slice(at + middle.length)
  return leading + translateUsageText(middle) + trailing
}

function walk(node: Node): void {
  if (node.nodeType === 3) {
    if (node.nodeValue) {
      const next = translated(node.nodeValue)
      if (next !== node.nodeValue) node.nodeValue = next
    }
    return
  }
  if (node.nodeType === 1) {
    const element = node as Element
    for (const name of ["aria-label", "title", "data-label"]) {
      const value = element.getAttribute(name)
      if (value) {
        const next = translateUsageText(value)
        if (next !== value) element.setAttribute(name, next)
      }
    }
  }
  for (const child of Array.from(node.childNodes)) walk(child)
}

export function traditionalChineseUsage(language: string): boolean {
  return /^zh-(?:Hant|TW|HK|MO)(?:-|$)/i.test(language)
}

/** Repaint copied and subsequently generated Usage copy after the legacy draw. */
export function translateUsage(root: Node, language: string): void {
  if (traditionalChineseUsage(language)) walk(root)
}
