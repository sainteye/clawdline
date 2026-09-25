/*
 * The words of the board, the Backlog and a session's to-dos (design-decisions
 * D35, T6).
 *
 * The Swift app never had these three structures, so its catalog has no words
 * for them: "Backlog", "Session 待辦" and "等你決定" are board-redesign §3.5's
 * names, and every sentence here says something the replicated screens never
 * said. They live in this one file, beside the page that says them, and the
 * copied `public/strings/zh-Hant.json` stays byte for byte. Where the catalog
 * already has the word — "已落地", "做完了，沒有落地", "在跑", "載入中",
 * "重新整理", "取消" — the page reads the catalog, not this.
 *
 * Holes are `{name}`, as the catalog's are.
 */

/**
 * The English catalog's own word, held once.
 *
 * Not a style choice: `refusals/audit.ts` finds E10 by searching this file for
 * a literal `tabBacklog: "Backlog"`, and a whole-file search cannot tell which
 * catalog a line sits in. With the English value held here, that literal can
 * only reappear by somebody writing the English word into another language —
 * which is the regression the rule is about. Inline it again and the rule goes
 * red on the one catalog that is right.
 */
const englishBacklog = "Backlog"

const words = {
  en: {
    nav: "Board",
    tabBoard: "Board",
    tabBacklog: englishBacklog,
    boardEyebrow: "BOARD · HAPPENING NOW",
    boardTitle: "What is happening now",
    boardLede:
      "What you need to know now. What the machine follows by itself is not here: it is in each session's detail, under Session to-dos.",
    backlogEyebrow: "BACKLOG · PLANNED",
    backlogTitle: englishBacklog,
    backlogLede: "Planned, not yet scheduled. Nothing leaves it unless you say so.",
    unreadable: "The board could not be read.",
    sweepStalled: "The board's sweep has stopped: a landing will not close its item until it runs again.",
    more: "{n} more",
    backProjects: "Back to Projects",
    scopeAll: "Scope: all projects",
    scopeProject: "Scope: {project}",
    scopePath: "Project path: {path}",
    scopeSource: "Project choices come from this machine's recognized project directory; projects found only in work data stay visible too.",
    projectDirectoryUnreadable: "The project directory could not be read, so choices from it may be missing.",
    workDirectoryUnreadable: "Some work lists could not be read, so the comparison between project sources is incomplete.",
    workOnlyProjects: "{n} projects occur in work data but not in the recognized project directory; they remain in the choices and are marked.",
    placesOnlyProjects: "{n} recognized projects do not occur in the loaded board, Backlog or proposal rows; they remain in the choices.",
    workCatalogPartial: "The work lists have more pages; project-source differences below cover only the rows loaded for this directory check.",
    workOnlyOption: "work data only",

    sectionDecide: "Deliveries to close, questions to decide",
    sectionActive: "In progress",
    sectionScheduled: "Scheduled this week",
    sectionDone: "Recently finished",
    sectionEmpty: "Nothing here right now.",
    decideLede: "Work already on this board whose delivery needs closing, and questions a session asked you.",

    toConfirm: "To confirm",
    toConfirmLede:
      "Questions still open. One leaves on its own when its subject settles — landed, or tracked another way — and " +
      "after 7 days unanswered the work stays with its session's to-dos.",
    toConfirmNone: "Nothing to confirm.",
    proposalKindLeftover: "Raised by delivery {task} — its own work is not what is being asked about",
    proposalLeaves: "Leaves on its own in {n} d",
    proposalLeavesToday: "Leaves on its own today",
    proposalGroupEffect: "Reaches outside this machine",
    proposalGroupStuck: "Owed for more than a day",
    proposalGroupLeftover: "Nobody has picked these up",
    proposalGroupOther: "Everything else",
    answerTrack: "Track",
    answerLater: "Later (Backlog)",
    answerNo: "No",

    decisionDefault: "If nobody answers by {when}: {option}",
    decisionBlocking: "Work is stopped until this is answered",
    decisionFrom: "Asked by {session}",

    digestTitle: "Daily digest",
    digestFor: "{date}",
    digestNone: "No digest yet. The first sweep of each day writes one for the day before.",
    digestCompleted: "{n} finished, {landed} of them landed",
    digestStalled: "{n} went quiet and back to the Backlog",
    digestFromBacklog: "{n} came onto the board from the Backlog",
    digestAutomatic: "{n} moved by a rule or a fact",
    digestProposals: "{n} proposals to confirm",
    digestDecisions: "{n} questions waiting for you",
    digestAwaiting: "{n} deliveries waiting to be closed",
    digestClosureAsked: "{n} deliveries asked about once; unanswered for 7 days, each closes as unconfirmed",
    digestHandedOff: "{n} to-dos nobody took",
    digestBacklogStale: "{n} Backlog items nobody has looked at for 30 days: keep them?",
    digestQuiet: "Nothing happened that day.",
    digestTruncated: "More moved that day than one digest reads; the totals are of the ones read.",

    owner: "Owner: {owner}",
    ownerYou: "you",
    tasks: "{n} tasks",
    tasksLive: "{n} running",
    tasksDelivered: "{n} delivered",
    tasksLanded: "{n} landed",
    tasksFailed: "{n} failed",
    tasksUnknown: "{n} unreadable",
    noTasks: "Nothing dispatched yet",
    lastEvidence: "Last evidence {when}",
    stallClock: "Back to the Backlog if nothing new happens by {when}",
    closureClock: "Closed as unconfirmed if nobody answers by {when}",
    startOn: "Starts {date}",
    reasonNoDispatch: "Nothing dispatched",
    reasonAttemptsFailed: "Every attempt failed",
    reasonRedispatched: "Dispatched again",
    reasonAccepted: "Accepted by you",
    reasonUnconfirmed: "Delivery not confirmed",
    reasonDropped: "Dropped",
    reasonDoneElsewhere: "Done, delivered off this item",
    rankNone: "Unranked",
    rank: "Rank {n}",
    timeline: "Timeline",

    opStart: "Start",
    opSchedule: "Schedule",
    opDefer: "Back to Backlog",
    opAccept: "Accept",
    opDoneElsewhere: "Done elsewhere",
    opRework: "Needs more work",
    opDrop: "Drop",
    opHandover: "Hand over",
    opUntrack: "Stop following",
    opRank: "Set rank",
    opDiscard: "Discard",
    handoverTo: "New owner (a session id, or user)",
    doneElsewhereWhy: "What was done, and where it landed",
    scheduleOn: "Start date",
    rankTo: "Rank (0 for none)",
    apply: "Apply",

    newItem: "New",
    newTitle: "What is it",
    newProject: "Project",
    newToBoard: "On the board: it starts now",
    newToBacklog: "In the Backlog: later",
    create: "Create",

    failed: "Not done.",
    failedConflict: "It changed while you were deciding; nothing was done. The board has been read again.",
    failedNetwork: "The daemon did not answer; nothing is known to have been done.",

    todosTitle: "Session to-dos",
    todoAddedBySession: "Added by Session",
    todoAddedBySessionLabel: "Added by this Session at your request; you did not send it",
    todosOpen: "{n} open",
    todosLede: "Made and closed by the broker's facts. Nothing here needs tending.",
    todosNone: "This session owes nothing.",
    todosUnknown: "This session's conversation is not known yet, so its to-dos cannot be named.",
    todosUnreadable: "This session's to-dos could not be read.",
    todosShowClosed: "Show closed",
    todosHideClosed: "Hide closed",
    todoDispatch: "Collect and land",
    todoOpen: "Open",
    todoHandedOff: "Handed off",
    todoDropped: "Let go",
    todoEscalated: "Worth asking you about",
    todoIncorporated: "Integrated by another landed task",
    createdViaSession: "Created by the Session from your message at {time}",
    createdViaQuote: "Your message",
    usageTitle: "Token bill",
    usageCardLabel: "What this item's tokens cost; open for the breakdown",
    usageLoading: "Reading the token bill…",
    usageUnreadable: "The token bill could not be read: {reason}",
    usageRetry: "Read again",
    usageTokens: "{n} tokens",
    usageCostPartial: "{cost} + {n} unpriced tokens",
    usageShareOfTokens: "Shares are of tokens: some model here has no price, so the whole cost is not known.",
    usageCalls: "{n} calls",
    usagePeak: "peak context {n}",
    usageCompactions: "{n} compactions",
    usageAbove: "{n} calls above 200k context, costing {cost}",
    usageAboveNone: "no call above 200k context",
    usageReadAt: "read {time}",
    usageMore: "The last pass stopped before the transcript's end; more is still to be read.",
    usageUpperBound: "upper bound",
    usageRulesWhy: "A guard run in the same command as other work counts the whole command, so rules is at most this much.",
    usageItemWhole: "A session that owned more than one item is counted whole in each, so this is at most what this item cost.",
    usageColCategory: "Category",
    usageColShare: "Share",
    usageColTokens: "Tokens",
    usageColCost: "Cost",
    usageNothingCounted: "Nothing is counted yet.",
    usageNoOwners: "No session has worked on this item yet.",
    usageSessions: "Sessions included",
    usageTasks: "Tasks included",
    usageSubagents: "{n} subagents, in delegate",
    usageGaps: "Not counted as a current reading",
    usageGapLine: "{kind} {id}: {reason}; {counted}",
    usageGapCounted: "its last reading is in the totals",
    usageGapNotCounted: "nothing of it is in the totals",
    usageKindSession: "Session",
    usageKindSubagent: "Subagent",
    usageKindTask: "Task",
    usageKindRootAssignment: "Root Assignment",
    usageKindOther: "{kind}",
    usageReasonNotYetRead: "not read yet",
    usageReasonTranscriptMissing: "its transcript is gone",
    usageReasonTranscriptUnreadable: "its transcript could not be read",
    usageSessionNotYetRead: "This session has not been read yet, so there is no bill to show.",
    usageSessionTranscriptMissing: "This session's transcript is gone; the figures are from its last reading.",
    usageSessionTranscriptUnreadable: "This session's transcript could not be read; any figures are from an earlier reading.",
    usageBase: "Base context — paid again by every call",
    usageBaseMeasured: "{n} tokens at the first call, divided by estimate",
    usageBaseUnknown: "The transcript did not record what the starting context was made of.",
    usageBaseSystemPrompt: "System prompt",
    usageBaseTools: "Tools",
    usageBaseSkills: "Skill listing",
    usageBaseInstructions: "Instruction files",
    usageBaseMcp: "MCP instructions",
    usageBaseOther: "Other",
  },
  "zh-Hant": {
    nav: "看板",
    tabBoard: "看板",
    tabBacklog: "後續工作",
    boardEyebrow: "看板 · 現在正在發生",
    boardTitle: "現在正在發生的事",
    boardLede: "你現在需要知道的事。機器自己在追的待辦不在這裡，在各個 session 詳情的「Session 待辦」裡。",
    backlogEyebrow: "後續工作 · 規劃中",
    backlogTitle: "後續工作",
    backlogLede: "已規劃，尚未排入。除非你說，這裡的東西不會被拿掉。",
    unreadable: "讀不到看板。",
    sweepStalled: "看板的巡檢停了：在它恢復之前，落地不會自動收掉對應的項目。",
    more: "還有 {n} 項",
    backProjects: "回到專案",
    scopeAll: "目前範圍：所有專案",
    scopeProject: "目前範圍：{project}",
    scopePath: "專案路徑：{path}",
    scopeSource: "專案選項來自這台機器認得的專案目錄；只在工作資料裡出現的專案也會保留。",
    projectDirectoryUnreadable: "讀不到專案目錄，因此可能缺少它提供的選項。",
    workDirectoryUnreadable: "有些工作清單讀不到，因此兩個專案來源的比對不完整。",
    workOnlyProjects: "有 {n} 個專案出現在工作資料、但不在機器認得的專案目錄裡；它們仍列在選項中，並且有標記。",
    placesOnlyProjects: "機器認得的專案中，有 {n} 個沒有出現在已載入的看板、後續工作或提議列裡；它們仍列在選項中。",
    workCatalogPartial: "工作清單還有下一頁；下方的來源差異只比對這次目錄檢查已載入的列。",
    workOnlyOption: "只在工作資料",

    sectionDecide: "交付待收尾、問題待決定",
    sectionActive: "進行中",
    sectionScheduled: "本週排入",
    sectionDone: "最近完成",
    sectionEmpty: "這一區目前沒有東西。",
    decideLede: "已在看板上的工作所留下、等著收尾的交付，以及 session 問你的事。",

    toConfirm: "待確認",
    toConfirmLede: "還成立的問題。主體落地或被別的方式追蹤了，它會自己退場；7 天沒有回答，這件事就只留在 session 的待辦裡。",
    toConfirmNone: "沒有要確認的。",
    proposalKindLeftover: "由交付 {task} 提出——被問的不是那個任務本身",
    proposalLeaves: "沒人回答的話，{n} 天後自己退場",
    proposalLeavesToday: "沒人回答的話，今天就自己退場",
    proposalGroupEffect: "會影響這台機器以外",
    proposalGroupStuck: "欠著超過一天",
    proposalGroupLeftover: "沒有人接手的事",
    proposalGroupOther: "其他",
    answerTrack: "追蹤",
    answerLater: "之後（後續工作）",
    answerNo: "不用",

    decisionDefault: "到 {when} 還沒人回答，就照「{option}」",
    decisionBlocking: "回答之前，工作停著",
    decisionFrom: "{session} 問的",

    digestTitle: "每日摘要",
    digestFor: "{date}",
    digestNone: "還沒有摘要。每天第一次巡檢時，會寫一份前一天的。",
    digestCompleted: "完成 {n} 件，其中 {landed} 件已落地",
    digestStalled: "{n} 件停擺，回到後續工作",
    digestFromBacklog: "{n} 件從後續工作移上看板",
    digestAutomatic: "{n} 次由規則或事實自動移動",
    digestProposals: "{n} 個提議待確認",
    digestDecisions: "{n} 個決定等你回答",
    digestAwaiting: "{n} 件交付等收尾",
    digestClosureAsked: "{n} 件交付問過你一次；7 天沒回答就以「交付未確認」結束",
    digestHandedOff: "{n} 筆待辦沒有人接",
    digestBacklogStale: "{n} 件後續工作 30 天沒人看過：要留嗎？",
    digestQuiet: "那一天沒有發生什麼事。",
    digestTruncated: "那一天的移動比一份摘要讀得多，上面的數字只算了讀到的部分。",

    owner: "負責：{owner}",
    ownerYou: "你",
    tasks: "{n} 個 task",
    tasksLive: "{n} 個在跑",
    tasksDelivered: "{n} 個已交付",
    tasksLanded: "{n} 個已落地",
    tasksFailed: "{n} 個失敗",
    tasksUnknown: "{n} 個讀不到",
    noTasks: "還沒有派工",
    lastEvidence: "最後一筆證據：{when}",
    stallClock: "到 {when} 還沒有新證據，就回到後續工作",
    closureClock: "到 {when} 還沒人回答，就以「交付未確認」結束",
    startOn: "預計 {date} 開始",
    reasonNoDispatch: "還沒有派工",
    reasonAttemptsFailed: "嘗試都失敗了",
    reasonRedispatched: "重新派工",
    reasonAccepted: "你收下了",
    reasonUnconfirmed: "交付未確認",
    reasonDropped: "已放棄",
    reasonDoneElsewhere: "做完了，交付沒綁上這一項",
    rankNone: "未排序",
    rank: "第 {n} 順位",
    timeline: "時間軸",

    opStart: "開始做",
    opSchedule: "排入",
    opDefer: "放回後續工作",
    opAccept: "收下",
    opDoneElsewhere: "做完了（沒綁上）",
    opRework: "還要改",
    opDrop: "放棄",
    opHandover: "轉交",
    opUntrack: "不用追蹤",
    opRank: "排序",
    opDiscard: "丟掉",
    handoverTo: "交給誰（session id，或 user）",
    doneElsewhereWhy: "做了什麼、在哪裡交付的",
    scheduleOn: "開始日期",
    rankTo: "順位（0 表示不排）",
    apply: "套用",

    newItem: "新增",
    newTitle: "要做什麼",
    newProject: "專案",
    newToBoard: "放上看板：現在就做",
    newToBacklog: "放進後續工作：之後再做",
    create: "建立",

    failed: "沒有成功。",
    failedConflict: "在你決定的時候它變了，所以什麼都沒做。看板已經重新讀過。",
    failedNetwork: "daemon 沒有回應；不知道有沒有做成。",

    todosTitle: "Session 待辦",
    todoAddedBySession: "Session 建立",
    todoAddedBySessionLabel: "這個 Session 依你的要求自己建立，不是你傳送的",
    todosOpen: "{n} 項未完成",
    todosLede: "由 broker 的事實自動建立、自動結束。你不需要整理它。",
    todosNone: "這個 session 沒有欠任何事。",
    todosUnknown: "還不知道這個 session 的對話是哪一個，所以說不出它的待辦。",
    todosUnreadable: "讀不到這個 session 的待辦。",
    todosShowClosed: "看已結束的",
    todosHideClosed: "收起已結束的",
    todoDispatch: "收結果、落地",
    todoOpen: "未完成",
    todoHandedOff: "已移交",
    todoDropped: "已放掉",
    todoEscalated: "值得問你",
    todoIncorporated: "已由另一個落地任務整合",
    createdViaSession: "Session 依你 {time} 的訊息建立",
    createdViaQuote: "你的訊息",
    usageTitle: "Token 帳單",
    usageCardLabel: "這個項目的 token 花費；打開看明細",
    usageLoading: "正在讀 token 帳單…",
    usageUnreadable: "讀不到 token 帳單：{reason}",
    usageRetry: "再讀一次",
    usageTokens: "{n} tokens",
    usageCostPartial: "{cost} + {n} 個沒有價格的 tokens",
    usageShareOfTokens: "占比以 token 計：這裡有模型沒有價格，所以總花費不完整。",
    usageCalls: "{n} 次呼叫",
    usagePeak: "最大 context {n}",
    usageCompactions: "壓縮 {n} 次",
    usageAbove: "{n} 次呼叫超過 200k context，花費 {cost}",
    usageAboveNone: "沒有呼叫超過 200k context",
    usageReadAt: "{time} 讀取",
    usageMore: "上一輪讀到一半就停了，對話紀錄還有沒讀完的部分。",
    usageUpperBound: "上限",
    usageRulesWhy: "守衛和其他工作在同一個指令裡跑時，整個指令都算進 rules，所以這是上限。",
    usageItemWhole: "負責過不只一個項目的 Session，會整個算進每個項目，所以這是這個項目花費的上限。",
    usageColCategory: "類別",
    usageColShare: "占比",
    usageColTokens: "Tokens",
    usageColCost: "花費",
    usageNothingCounted: "還沒有算進任何東西。",
    usageNoOwners: "還沒有 Session 做過這個項目。",
    usageSessions: "算進來的 Session",
    usageTasks: "算進來的任務",
    usageSubagents: "{n} 個 subagent，算在 delegate",
    usageGaps: "沒有算到最新的部分",
    usageGapLine: "{kind} {id}：{reason}；{counted}",
    usageGapCounted: "上一次讀到的數字有算進總數",
    usageGapNotCounted: "總數裡完全沒有它",
    usageKindSession: "Session",
    usageKindSubagent: "Subagent",
    usageKindTask: "任務",
    usageKindRootAssignment: "Root Assignment",
    usageKindOther: "{kind}",
    usageReasonNotYetRead: "還沒讀到",
    usageReasonTranscriptMissing: "對話紀錄已經不在",
    usageReasonTranscriptUnreadable: "對話紀錄讀不了",
    usageSessionNotYetRead: "這個 Session 還沒被讀過，所以還沒有帳單。",
    usageSessionTranscriptMissing: "這個 Session 的對話紀錄已經不在；數字是最後一次讀到的。",
    usageSessionTranscriptUnreadable: "這個 Session 的對話紀錄讀不了；若有數字，是先前讀到的。",
    usageBase: "基底 context——每次呼叫都會再付一次",
    usageBaseMeasured: "第一次呼叫時 {n} tokens，以估算拆分",
    usageBaseUnknown: "對話紀錄沒有記下起始 context 由什麼組成。",
    usageBaseSystemPrompt: "System prompt",
    usageBaseTools: "工具",
    usageBaseSkills: "Skill 清單",
    usageBaseInstructions: "指示檔",
    usageBaseMcp: "MCP 指示",
    usageBaseOther: "其他",
  },
} as const

export type WorkWord = keyof (typeof words)["en"]

/** The card needs a project name; an absolute machine path adds no identity. */
export function workProjectName(project: string): string {
  const parts = project.split(/[\\/]+/).filter(Boolean)
  return parts.at(-1) ?? project
}

/** The page's language as the copied catalog set it, or the browser's (next-strings.ts). */
export function language(): "en" | "zh-Hant" {
  const lang = (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) || "en"
  return lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"
}

/** One sentence, its holes filled. */
export function workWord(key: WorkWord, holes: Record<string, string | number> = {}): string {
  return workWordIn(language(), key, holes)
}

/** One sentence in a named language, its holes filled. */
export function workWordIn(lang: "en" | "zh-Hant", key: WorkWord, holes: Record<string, string | number> = {}): string {
  return words[lang][key].replace(/\{(\w+)\}/g, (all, name: string) =>
    name in holes ? String(holes[name]) : all,
  )
}

/** The person's message a Session created an item on, as the item wire carries it. */
export interface CreatedVia {
  run: string
  session_id: string
  at: number
  excerpt?: string
}

/** A Unix time as this browser's local HH:MM. */
export function clockOf(at: number): string {
  const d = new Date(at * 1000)
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`
}

/**
 * The line a card created by a Session on the person's message carries, or
 * null for an item the person created themselves.
 */
export function createdViaLine(via: CreatedVia | null | undefined, lang: "en" | "zh-Hant" = language()): string | null {
  if (!via || !via.run || !via.at) return null
  return workWordIn(lang, "createdViaSession", { time: clockOf(via.at) })
}
