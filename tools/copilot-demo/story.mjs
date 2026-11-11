// The invented story the voice copilot demo tells, written separately in
// English and Taiwan Traditional Chinese so neither reads as a translation.
// docs/copilot-demo.md is the whole of it.
//
// Six Sessions in two projects: two wait on the person, two are working, two
// have delivered. The call reads the briefing, has a finished Session's whole
// story told, opens Release 2.4 and sends it a message the viewer sees arrive.

export const IDS = {
  checkout: 'demo-checkout', webhooks: 'demo-webhooks', emails: 'demo-emails',
  release: 'demo-release', sync: 'demo-sync', search: 'demo-search',
}
const SHOP = '~/code/storefront', APP = '~/code/mobile-app'

const MARK = ['.######.', '.#o##o#.', '########', '.######.']
const icon = (c) => ({ accent: c, cells: MARK.map((r) => [...r].map((ch) => ch === '#' ? c : ch === 'o' ? '#141416' : null)) })
const COLORS = { [IDS.checkout]: '#e07a5f', [IDS.webhooks]: '#8fd06b', [IDS.emails]: '#f2c14e', [IDS.release]: '#5fa8e0', [IDS.sync]: '#a371f7', [IDS.search]: '#e0669b' }

export const PLACES = [
  { id: 'demo-storefront', label: 'storefront', path: SHOP, icon: { accent: '#e07a5f', cells: [['#e07a5f', '#e07a5f'], ['#e07a5f', '#141416']] } },
  { id: 'demo-mobile', label: 'mobile-app', path: APP, icon: { accent: '#5fa8e0', cells: [['#5fa8e0', '#141416'], ['#5fa8e0', '#5fa8e0']] } },
]

const WORDS = {
  en: {
    names: { checkout: 'Checkout redesign', webhooks: 'Payment webhooks', emails: 'Order emails', release: 'Release 2.4', sync: 'Offline sync', search: 'Search rebuild' },
    checkoutQ: 'The new checkout passes all 212 tests. Merge it into main now, or wait for the design review?',
    checkoutOptions: [['Merge now', 'Open the PR and merge into main'], ['Wait for review', 'Keep the branch until design signs off']],
    releaseQ: '3 UI tests fail on iPad only (CheckoutSheet, PhotoPicker, SettingsSplit). Skip them for 2.4, or fix them first?',
    releaseOptions: [['Skip for 2.4', 'Mark them known issues'], ['Fix first', 'Hold the release until they pass']],
    emailsDelivered: 'Order emails render in dark mode, the Outlook logo is fixed, and 14 tests cover them.',
    searchDelivered: 'Meilisearch search is live for every shopper and the old SQL search is removed.',
    send: "Fix the three iPad UI tests first; don't skip them.",
    briefing: 'Two Sessions need you. Checkout redesign is done and asks whether to merge into main now or wait for the design review. Release 2.4 has three UI tests failing on iPad only and asks whether to skip them for 2.4 or fix them first. Payment webhooks is building the retry queue; Offline sync is writing conflict resolution. Order emails and Search rebuild have delivered.',
    transcripts: {
      checkout: [['user', 'Rebuild the checkout as a single page: address, shipping and payment together, with Apple Pay first.', 7200],
        ['assistant', 'The single-page checkout is done. Apple Pay is the first option, address and shipping validate inline, and all 212 tests pass.\n\n{checkoutQ}', 120]],
      webhooks: [['user', 'Make the Stripe webhook handler idempotent and retry failed deliveries with backoff.', 2400],
        ['assistant', 'Idempotency keys are stored per event now. Working on the retry queue with exponential backoff.', 60]],
      emails: [['user', 'Order confirmation emails look broken in dark mode. Fix them.', 9000], ['report', 'Order emails render correctly in dark mode.', 6000],
        ['user', 'The logo is broken in Outlook.', 5000], ['report', 'Fixed the Outlook logo with a PNG fallback.', 3500],
        ['user', 'Add tests so this does not break again.', 3000], ['report', '{emailsDelivered}', 1500],
        ['assistant', 'Done: dark mode, the Outlook logo fallback, and 14 snapshot tests across Gmail, Outlook and Apple Mail.', 1500]],
      release: [['user', 'Prepare the 2.4 release: bump the versions, run the full UI suite and draft the release notes.', 3000],
        ['assistant', 'Versions are bumped to 2.4.0 and the release notes are drafted. The UI suite passes on iPhone, but 3 tests fail on iPad only: CheckoutSheet, PhotoPicker and SettingsSplit.\n\n{releaseQ}', 240]],
      sync: [['user', 'Queue edits while offline and replay them when the connection returns.', 4000],
        ['assistant', 'The queue persists to disk now. Writing the conflict resolution for edits made on two devices.', 360]],
      search: [['user', 'Search is too slow and misses obvious matches. Replace the SQL LIKE search with Meilisearch.', 30000],
        ['assistant', 'Meilisearch runs beside the API now, and the product endpoint queries it instead of SQL. Next I need an index that stays current.', 27000],
        ['user', 'Keep the index in sync when products change.', 26000], ['report', 'Built the indexing job: product changes reach the search index within two seconds.', 22000],
        ['user', 'Customers misspell things all the time. Add typo tolerance, and filters for size and color.', 21000], ['report', 'Added typo tolerance and size and color filters.', 17000],
        ['user', 'Load test it before we ship anything.', 16000],
        ['assistant', 'Load test at 200 requests per second: p95 latency went from 1.8 s on SQL to 120 ms on Meilisearch, with no errors.', 14000],
        ['user', 'Ship it behind a feature flag to 10% of shoppers first.', 13000], ['report', 'Shipped the new search behind a flag to 10% of shoppers.', 11000],
        ['user', 'No complaints after two days. Roll it out to everyone and delete the old search.', 4000], ['report', '{searchDelivered}', 3000],
        ['assistant', 'Done. New search is at 100%, the flag is removed, and the old SQL search code and its tests are deleted (1,240 lines).', 3000]],
    },
    // What each voice says: [voice, words]. The copilot is marin, the person cedar.
    lines: {
      c0: ['marin', 'Hi! I can tell you how every Session on this machine is doing.'],
      p1: ['cedar', "What's going on right now?"],
      c1: ['marin', 'Two things need you. Checkout redesign is done, and asks whether to merge it into main now. Release 2.4 has three failing iPad tests, and asks whether to skip them or fix them first. Payment webhooks is still working on retries.'],
      pS: ['cedar', 'Open Search rebuild and walk me through what it did.'],
      cS: ['marin', 'It started by replacing the slow SQL search with Meilisearch. Next it built the indexing job, and added typo tolerance and filters for size and color. A load test then took search from 1.8 seconds to 120 milliseconds, so it shipped behind a feature flag to ten percent of shoppers. Finally, it rolled out to everyone and removed the old search code.'],
      p2o: ['cedar', 'Open Release 2.4 for me.'],
      c5: ['marin', 'Release 2.4 is open.'],
      p3o: ['cedar', "Tell it to fix the iPad tests first. Don't skip them."],
      c3: ['marin', "I'll send Release 2.4: fix the three iPad UI tests first, don't skip them. Should I send it?"],
      p4: ['cedar', 'Yes, send it.'],
      c4: ['marin', 'Sent. Release 2.4 is on it.'],
      p6: ['cedar', "Perfect, that's all for now."],
      c6: ['marin', 'Sounds good. Talk soon!'],
    },
    voiceStyle: {
      marin: 'You are a friendly, calm voice assistant on a phone call, reporting to a colleague. Natural, warm, conversational American English; clear but not slow; never sound like a narrator.',
      cedar: 'You are a software engineer talking casually to your assistant on a call. Relaxed, natural American English.',
    },
  },
  'zh-Hant': {
    names: { checkout: '結帳改版', webhooks: '金流 Webhook', emails: '訂單通知信', release: '2.4 版發布', sync: '離線同步', search: '搜尋重建' },
    checkoutQ: '新的結帳流程 212 個測試全部通過。要現在合併到 main，還是等設計審查？',
    checkoutOptions: [['現在合併', '開 PR 並合併到 main'], ['等設計審查', '分支先留著，等設計確認']],
    releaseQ: '有 3 個 UI 測試只在 iPad 上失敗（CheckoutSheet、PhotoPicker、SettingsSplit）。2.4 先略過，還是先修好？',
    releaseOptions: [['2.4 先略過', '列為已知問題'], ['先修好', '測試通過前先不發布']],
    emailsDelivered: '訂單通知信在深色模式下顯示正常，修好 Outlook 的 Logo，並補上 14 個測試。',
    searchDelivered: 'Meilisearch 搜尋已對所有顧客上線，舊的 SQL 搜尋已移除。',
    send: '先把三個 iPad UI 測試修好，不要略過。',
    briefing: '有兩個 Session 在等你。結帳改版已經完成，在問要現在合併到 main 還是等設計審查。2.4 版發布有三個 UI 測試只在 iPad 上失敗，在問 2.4 要先略過還是先修好。金流 Webhook 正在做重試佇列；離線同步正在寫衝突處理。訂單通知信和搜尋重建都已經交付。',
    transcripts: {
      checkout: [['user', '把結帳改成單一頁面：地址、運送和付款放在一起，Apple Pay 放第一個。', 7200],
        ['assistant', '單頁結帳做好了。Apple Pay 排在第一個，地址和運送方式即時檢查，212 個測試全部通過。\n\n{checkoutQ}', 120]],
      webhooks: [['user', '讓 Stripe 的 webhook 處理可以重複執行不出錯，送失敗的要退避重試。', 2400],
        ['assistant', '每個事件都會記下 idempotency key 了。現在在做指數退避的重試佇列。', 60]],
      emails: [['user', '訂單確認信在深色模式下整個跑版，修一下。', 9000], ['report', '訂單通知信在深色模式下顯示正常。', 6000],
        ['user', 'Outlook 裡的 Logo 壞掉了。', 5000], ['report', '用 PNG 備援修好 Outlook 的 Logo。', 3500],
        ['user', '補測試，之後不要再壞。', 3000], ['report', '{emailsDelivered}', 1500],
        ['assistant', '完成：深色模式、Outlook Logo 備援，以及涵蓋 Gmail、Outlook 和 Apple Mail 的 14 個快照測試。', 1500]],
      release: [['user', '準備 2.4 版：更新版本號、跑完整的 UI 測試、寫好發布說明。', 3000],
        ['assistant', '版本號已更新到 2.4.0，發布說明也寫好了。UI 測試在 iPhone 上全部通過，但有 3 個只在 iPad 上失敗：CheckoutSheet、PhotoPicker 和 SettingsSplit。\n\n{releaseQ}', 240]],
      sync: [['user', '離線時把修改排進佇列，連線恢復後再重送。', 4000],
        ['assistant', '佇列已經會存到磁碟了。現在在寫兩台裝置同時修改時的衝突處理。', 360]],
      search: [['user', '搜尋太慢，明顯的結果也找不到。把 SQL LIKE 搜尋換成 Meilisearch。', 30000],
        ['assistant', 'Meilisearch 已經跟 API 一起跑起來，商品搜尋改查它，不再查 SQL。接下來需要一個隨時同步的索引。', 27000],
        ['user', '商品有變動時，索引要跟著更新。', 26000], ['report', '建好索引同步工作：商品變動兩秒內就會反映到搜尋索引。', 22000],
        ['user', '顧客常常打錯字。加上錯字容忍，還有尺寸和顏色篩選。', 21000], ['report', '加上錯字容忍，以及尺寸和顏色篩選。', 17000],
        ['user', '上線前先做壓力測試。', 16000],
        ['assistant', '每秒 200 個請求的壓力測試：p95 延遲從 SQL 的 1.8 秒降到 Meilisearch 的 120 毫秒，沒有任何錯誤。', 14000],
        ['user', '先用功能開關開給一成的顧客。', 13000], ['report', '新搜尋已用功能開關開放給一成顧客。', 11000],
        ['user', '兩天都沒有客訴。全面開放，把舊的搜尋刪掉。', 4000], ['report', '{searchDelivered}', 3000],
        ['assistant', '完成。新搜尋已全面開放，功能開關已移除，舊的 SQL 搜尋程式和測試都刪掉了（1,240 行）。', 3000]],
    },
    lines: {
      c0: ['marin', '嗨，我可以跟你報告這台電腦上每個 Session 的狀況。'],
      p1: ['cedar', '現在狀況怎麼樣？'],
      c1: ['marin', '有兩件事要你處理。結帳改版已經做完，在問要不要現在合併到 main。2.4 版發布有三個 iPad 測試沒過，在問要先略過還是先修好。金流 Webhook 還在做重試。'],
      pS: ['cedar', '打開搜尋重建，跟我講它做了哪些事。'],
      cS: ['marin', '一開始，它把很慢的 SQL 搜尋換成 Meilisearch。接著建好索引同步，又加上錯字容忍，還有尺寸跟顏色的篩選。後來壓力測試顯示，搜尋從 1.8 秒降到 120 毫秒，所以先用功能開關開給一成的顧客。最後全面上線，舊的搜尋程式也刪掉了。'],
      p2o: ['cedar', '幫我打開 2.4 版發布。'],
      c5: ['marin', '2.4 版發布已經打開了。'],
      p3o: ['cedar', '跟它說先把 iPad 測試修好，不要略過。'],
      c3: ['marin', '我會傳給 2.4 版發布：先把三個 iPad UI 測試修好，不要略過。要送出嗎？'],
      p4: ['cedar', '好，送出。'],
      c4: ['marin', '送出了，2.4 版發布已經開始處理。'],
      p6: ['cedar', '很好，先這樣。'],
      c6: ['marin', '好，有事再找我！'],
    },
    voiceStyle: {
      marin: '你是語音助理，在電話裡跟熟悉的同事報告。用台灣口音的自然口語中文，語氣親切、從容、清楚，不要播音腔，也不要太慢。',
      cedar: '你是一位台灣的軟體工程師，在電話裡隨口跟助理講話。台灣口音、自然、放鬆、口語。',
    },
  },
}

export const LANGS = Object.keys(WORDS)

/** The story in `lang`, with the mutable part (whether the message has been sent). */
export function story(lang) {
  const w = WORDS[lang]
  if (!w) throw new Error(`no story in ${lang}; have ${LANGS.join(', ')}`)
  const now = Math.floor(Date.now() / 1000)
  const state = { releaseSent: false, sentText: '' }
  const fill = (s) => s.replace(/\{(\w+)\}/g, (_, k) => w[k])
  const menu = (question, options) => ({ question, options: options.map(([label, detail], i) => ({ n: i + 1, label, detail, can: true, selected: false })) })
  const row = (key, minutesAgo, extra = {}) => ({
    id: IDS[key], label: w.names[key], backend: 'tmux', state: 'idle', work_state: 'ready', evidence: 'process',
    isClaude: true, assistant: 'claude', sessionId: 'conversation-' + IDS[key], cwd: SHOP, icon: icon(COLORS[IDS[key]]),
    activity: { known: true, at: now - minutesAgo * 60 },
    closeability: { activity_generation: 1, attestation_id: null, mover: null, obligation_generation: 1, observed_at: 1,
      provenance: [], reasons: [], session_generation: 1, source: 'broker', state: 'safe', version: 'demo' },
    ...extra,
  })
  const delivered = (title, secondsAgo) => ({ disposition: { evidence: 'authenticated_session_delivery', receiptAt: now - secondsAgo, scope: 'session', title } })
  const codex = { assistant: 'codex', isClaude: false }
  return {
    lang, words: w, state, places: PLACES, lines: w.lines, briefing: w.briefing, send: w.send,
    sessions: () => [
      row('checkout', 2, { state: 'waiting', work_state: 'waiting_you', menu: menu(w.checkoutQ, w.checkoutOptions) }),
      row('webhooks', 1, { ...codex, state: 'working', work_state: 'working', line: '14m' }),
      row('emails', 25, delivered(w.emailsDelivered, 1500)),
      row('release', state.releaseSent ? 0 : 4, { cwd: APP, ...codex, ...(state.releaseSent
        ? { state: 'working', work_state: 'working', line: '0m' }
        : { state: 'waiting', work_state: 'waiting_you', menu: menu(w.releaseQ, w.releaseOptions) }) }),
      row('sync', 6, { cwd: APP, state: 'working', work_state: 'working', line: '31m' }),
      row('search', 50, delivered(w.searchDelivered, 3000)),
    ],
    transcript(id) {
      const key = Object.keys(IDS).find((k) => IDS[k] === id)
      const entries = (w.transcripts[key] ?? []).map(([role, text, ago]) => role === 'report'
        // A delivery as the transcript records it: the Session ran `clawdline session report`.
        ? { role: 'tool', text: `clawdline session report --summary "${fill(text)}"`, at: now - ago, kind: 'tool' }
        : { role, text: fill(text), at: now - ago, kind: 'message' })
      if (key === 'release' && state.releaseSent) entries.push({ role: 'user', text: state.sentText, at: Math.floor(Date.now() / 1000), kind: 'message' })
      return entries
    },
  }
}

/**
 * The call, step by step. `hold` waits for the recorder (which shrinks the
 * sheet to its bubble); `tool` is run by the console's own client against the
 * stand-in daemon, and the copilot's next line waits for its result.
 */
export function steps(send) {
  return [
    { copilot: 'c0' }, { pause: 900, person: 'p1' }, { pause: 300, copilot: 'c1' },
    { pause: 700, hold: 'minimize' },
    { pause: 1200, person: 'pS' }, { tool: 'open_session', args: { session_id: IDS.search } },
    { pause: 300, tool: 'get_session_recap', args: { session_id: IDS.search } }, { pause: 600, copilot: 'cS' },
    { pause: 1300, person: 'p2o' }, { tool: 'open_session', args: { session_id: IDS.release } }, { pause: 300, copilot: 'c5' },
    { pause: 1200, person: 'p3o' }, { pause: 300, copilot: 'c3' }, { pause: 700, person: 'p4' },
    { tool: 'send_message_to_session', args: { session_id: IDS.release, message: send } }, { pause: 500, copilot: 'c4' },
    { pause: 1300, person: 'p6' }, { pause: 300, copilot: 'c6' },
  ]
}
