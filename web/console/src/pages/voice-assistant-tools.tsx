import { useMemo, useState } from 'react'
import { COPILOT_TOOLS } from '../realtime/copilot-tools.js'
import { nextWord as W } from '../next-strings.js'

export type ToolCategory = 'all' | 'status' | 'session' | 'voice' | 'tools'

interface ToolMeta {
  titleZh: string
  titleEn: string
  category: 'status' | 'session' | 'voice' | 'tools'
  summaryZh: string
  summaryEn: string
  triggersZh: string[]
  triggersEn: string[]
}

const TOOL_METAS: Record<string, ToolMeta> = {
  get_status_briefing: {
    titleZh: '整理現況匯報',
    titleEn: 'Status Briefing',
    category: 'status',
    summaryZh: '取得機器上依輕重緩急整理好的整體現況：包含需你確認的事項、進行中的任務與最近交付。',
    summaryEn: 'Get a prioritized briefing of pending decisions, active work, and recent deliveries.',
    triggersZh: ['匯報現在的狀況', '現在怎麼樣', '有什麼要我處理'],
    triggersEn: ['Give me a status briefing', 'What needs my attention?', 'How are things going?'],
  },
  list_sessions: {
    titleZh: '查詢 Sessions 清單',
    titleEn: 'List Sessions',
    category: 'status',
    summaryZh: '快速查詢所有 Sessions 的名稱、目前工作狀態（工作中、等回覆、閒置）與既有交付摘要。',
    summaryEn: 'Quickly list session names, states (working, waiting, idle), and recent delivery summaries.',
    triggersZh: ['有哪些任務在跑', '列出現在的 Session', '看所有進度'],
    triggersEn: ['What sessions are running?', 'List all sessions', 'Show all tasks'],
  },
  get_sessions_summary: {
    titleZh: '多個 Session 摘要',
    titleEn: 'Group Sessions Summary',
    category: 'status',
    summaryZh: '彙整一群 Session 各自的目標、最近完成工作與目前狀態，依最新活動時間依序說明。',
    summaryEn: 'Summarize a group of sessions by target, latest completions, and current activity.',
    triggersZh: ['工作中的都在做什麼', '最近完成的那些做了什麼', '全部 Session 的摘要'],
    triggersEn: ['What are working sessions doing?', 'Summarize recent sessions', 'Summary of all sessions'],
  },
  get_session_recap: {
    titleZh: 'Session 完整歷程與進度',
    titleEn: 'Full Session Recap',
    category: 'status',
    summaryZh: '讀取特定 Session 從開始到現在的完整經過，包含看板步驟進度、歷次交付回報與最終回覆。',
    summaryEn: 'Read the complete journey of a session, including board step progress and deliveries.',
    triggersZh: ['這個 Session 完成了什麼', '從頭到尾做了哪些事', '幫我總結這個任務', '還剩幾項步驟'],
    triggersEn: ['What did this session accomplish?', 'Summarize this session from the start', 'How many steps remain?'],
  },
  get_session_summary: {
    titleZh: '單一 Session 目前摘要',
    titleEn: 'Single Session Summary',
    category: 'status',
    summaryZh: '查詢指定 Session 的目標、目前正在進行的具體工作、以及最近完成的進展。',
    summaryEn: 'Check what a specific session is working on right now and its latest progress.',
    triggersZh: ['這個 Session 現在在做什麼', '那台在跑什麼', '它做到哪裡了'],
    triggersEn: ['What is this session doing now?', 'What is that running?', 'Where is it at?'],
  },
  get_session_activity: {
    titleZh: '終端動態與未讀紀錄',
    titleEn: 'Terminal Activity & Log',
    category: 'status',
    summaryZh: '讀取指定 Session 的終端畫面、最近幾行輸出、正在跑的指令與尚未讀取的對話紀錄。',
    summaryEn: 'Inspect live terminal command execution, recent output lines, and unread transcript entries.',
    triggersZh: ['這台有沒有動', '剛剛那行指令有沒有報錯', '它是不是卡住了', '終端機現在在跑什麼'],
    triggersEn: ['Is it still moving?', 'Did that command fail?', 'Is it stuck?', 'What command is running?'],
  },
  explain_session_reply: {
    titleZh: '口語解釋最後回覆',
    titleEn: 'Explain Latest Reply',
    category: 'session',
    summaryZh: '讀取助理最新的一則回覆，並以自然流暢的口語向你清楚解釋核心答案與重點。',
    summaryEn: 'Read the assistant’s latest reply and explain its key message in natural speech.',
    triggersZh: ['它剛剛回了什麼', '那台回覆說什麼', '它的最後答案是什麼'],
    triggersEn: ['What did it just reply?', 'What did that assistant say?', 'What was its final answer?'],
  },
  get_session_screen: {
    titleZh: '讀取終端當前畫面',
    titleEn: 'Read Terminal Screen',
    category: 'session',
    summaryZh: '取得指定 Session 當前終端視窗顯示的文字畫面（包含最新輸出與提示字元）。',
    summaryEn: 'Capture the visible terminal screen text for a specific session.',
    triggersZh: ['看看終端機畫面', '終端機現在有什麼', '畫面停在哪裡'],
    triggersEn: ['Check the terminal screen', 'What is on screen right now?', 'Where did it stop?'],
  },
  open_session: {
    titleZh: '切換螢幕畫面至 Session',
    titleEn: 'Open Session On Screen',
    category: 'session',
    summaryZh: '在當前瀏覽器分頁中，將視角切換至指定 Session，直接在畫面上呈現該任務視窗。',
    summaryEn: 'Switch the active browser window/page to display the specified session.',
    triggersZh: ['切過去', '幫我開那個 Session', '切到某某任務', '看看那台的畫面'],
    triggersEn: ['Switch to it', 'Open that session for me', 'Bring up that task'],
  },
  answer_session_question: {
    titleZh: '回答 Session 提問',
    titleEn: 'Answer Session Question',
    category: 'session',
    summaryZh: '當 Session 停在詢問畫面（選擇題、yes/no、確認操作等）等待時，以語音送出你的選擇。',
    summaryEn: 'Respond to interactive prompts (choices, confirmations) waiting in a session.',
    triggersZh: ['選第一個', '同意', '確認執行', '回答第二個選項'],
    triggersEn: ['Choose the first option', 'Yes, proceed', 'Confirm that choice'],
  },
  get_session_updates: {
    titleZh: '自上次報告後的最新進展',
    titleEn: 'Get Session Updates',
    category: 'status',
    summaryZh: '讀取指定 Session 自上一次向你回報之後，新增的終端指令、輸出或助理回覆。',
    summaryEn: 'Read new terminal commands, outputs, and assistant messages since the last report.',
    triggersZh: ['它剛剛之後有沒有新進展', '這段時間有什麼更新', '之後有動靜嗎'],
    triggersEn: ['Any updates since you last checked?', 'What happened since then?'],
  },
  get_session_notes: {
    titleZh: '讀取工作筆記',
    titleEn: 'Read Session Notes',
    category: 'session',
    summaryZh: '查看指定 Session 所記錄的工作筆記、重點備忘與結構化記錄。',
    summaryEn: 'Read saved notes, checklist memos, and structured records from a session.',
    triggersZh: ['它有沒有留筆記', '有什麼記下來的事項', '幫我看一下筆記'],
    triggersEn: ['Are there any notes?', 'What was written down?', 'Check the notes for me'],
  },
  list_snippets: {
    titleZh: '列出可用 Snippets 片段',
    titleEn: 'List Snippets',
    category: 'tools',
    summaryZh: '查詢指定 Session 可使用的程式碼片段、常用指令碼與快捷操作清單。',
    summaryEn: 'List available code snippets, frequent command sequences, and shortcuts.',
    triggersZh: ['有什麼指令片段可以用', '有哪些 snippet', '看常用指令碼'],
    triggersEn: ['What snippets are available?', 'List snippets for this session'],
  },
  open_snippets: {
    titleZh: '開啟 Snippets 面板',
    titleEn: 'Open Snippets Panel',
    category: 'tools',
    summaryZh: '在當前頁面上開啟指定 Session 的 Snippets 面板或切換至程式碼片段分頁。',
    summaryEn: 'Open the Snippets panel or navigate to the snippets view on screen.',
    triggersZh: ['打開 snippets 面板', '查看指令片段分頁'],
    triggersEn: ['Open the snippets panel', 'Show snippet tab'],
  },
  run_snippet: {
    titleZh: '執行指定 Snippet 片段',
    titleEn: 'Run Snippet',
    category: 'tools',
    summaryZh: '在指定的 Session 中觸發並執行一組預先定義的指令片段或自動化操作。',
    summaryEn: 'Trigger and run a pre-defined snippet or automation sequence in the session.',
    triggersZh: ['執行某某 snippet', '跑那個檢查腳本', '執行這個片段'],
    triggersEn: ['Run snippet X', 'Execute the check snippet'],
  },
  clawdline_help: {
    titleZh: '查詢 Clawdline 使用指南',
    titleEn: 'Clawdline Help Guide',
    category: 'tools',
    summaryZh: '查詢 Clawdline 系統操作手冊、功能說明、快捷鍵與使用技巧。',
    summaryEn: 'Look up the Clawdline user guide, feature explanations, shortcuts, and tips.',
    triggersZh: ['這個功能怎麼用', 'Clawdline 是什麼', '要怎麼設定機器'],
    triggersEn: ['How do I use this feature?', 'What is Clawdline?', 'How do I pair a machine?'],
  },
  get_devstack_links: {
    titleZh: '取得 Dev Stack 開發服務連結',
    titleEn: 'Get Dev Stack Links',
    category: 'tools',
    summaryZh: '查詢指定專案的 Dev Stack 服務網址、預覽端口與相關開發工具連結。',
    summaryEn: 'Fetch preview URLs, dev service ports, and development links for a project.',
    triggersZh: ['開發網址是什麼', '幫我開 devstack', '本地服務網址在哪裡'],
    triggersEn: ['What is the dev URL?', 'Open devstack for me', 'Show local preview link'],
  },
  set_speaking_speed: {
    titleZh: '調整語音說話速度',
    titleEn: 'Set Speaking Pace',
    category: 'voice',
    summaryZh: '即時調整語音副駕駛的說話速度（偏慢 0.8、從容 0.9、正常 1.0、偏快 1.1）。',
    summaryEn: 'Adjust the voice speaking pace in real time (slow, calm, normal, quick).',
    triggersZh: ['講快一點', '講慢一點', '說慢一點', '用正常速度說話'],
    triggersEn: ['Speak faster', 'Speak a bit slower', 'Normal speed please'],
  },
  set_answer_length: {
    titleZh: '設定回答風格與長度',
    titleEn: 'Set Answer Length',
    category: 'voice',
    summaryZh: '切換語音回答的簡練程度（精簡摘要風格或詳細完整風格）。',
    summaryEn: 'Switch voice response brevity (concise summary vs. detailed explanation).',
    triggersZh: ['講簡短一點', '只要重點', '詳細一點跟我說'],
    triggersEn: ['Keep it brief', 'Just the highlights', 'Give me more details'],
  },
  get_call_status: {
    titleZh: '查詢目前通話狀態',
    titleEn: 'Get Call Status',
    category: 'voice',
    summaryZh: '查詢當前通話的連線品質、通話時間與目前音訊通道狀態。',
    summaryEn: 'Check active call duration, connection state, and audio channel status.',
    triggersZh: ['我們講多久了', '現在通話狀態如何', '連線順暢嗎'],
    triggersEn: ['How long have we been talking?', 'What is the call status?'],
  },
  end_call: {
    titleZh: '結束語音通話',
    titleEn: 'End Voice Call',
    category: 'voice',
    summaryZh: '掛斷並結束當前的 WebRTC 語音通話，關閉音訊通道。',
    summaryEn: 'Hang up and close the active voice call session.',
    triggersZh: ['就先這樣', '再見', '掛斷電話', '掰掰'],
    triggersEn: ['That is all for now', 'Goodbye', 'Hang up the call', 'Bye'],
  },
  smart_rename_sessions: {
    titleZh: '智慧整理 Session 標題',
    titleEn: 'Smart Rename Sessions',
    category: 'session',
    summaryZh: '根據 Session 實際執行工作內容與近期交付，自動更新成精確有意義的標題。',
    summaryEn: 'Intelligently rename sessions according to actual work and recent deliveries.',
    triggersZh: ['幫我整理 Session 名字', '重新命名一下任務', '名字太亂了整理一下'],
    triggersEn: ['Clean up session titles', 'Rename sessions intelligently', 'Tidy up session names'],
  },
  close_session: {
    titleZh: '關閉指定 Session',
    titleEn: 'Close Session',
    category: 'session',
    summaryZh: '關閉指定的 Session 任務視窗，若有未存工作會進行保護確認。',
    summaryEn: 'Close a specific session cleanly, with safeguards if work is unfinished.',
    triggersZh: ['把這個 Session 關掉', '結束那個任務', '關閉某某 session'],
    triggersEn: ['Close this session', 'Terminate that task', 'Close session X'],
  },
  list_machines: {
    titleZh: '列出已配對機器',
    titleEn: 'List Paired Machines',
    category: 'tools',
    summaryZh: '查詢帳號下所有已配對的電腦裝置名稱、線上狀態與當前本機識別。',
    summaryEn: 'List all paired devices, online status, and current local machine flag.',
    triggersZh: ['有哪幾台電腦在線上', '我的機器有連上嗎', '看所有配對機器'],
    triggersEn: ['What machines are online?', 'Is my machine connected?', 'List all paired computers'],
  },
  list_projects: {
    titleZh: '查詢專案清單',
    titleEn: 'List Projects',
    category: 'tools',
    summaryZh: '查詢指定機器上的專案目錄、工作區路徑與可用專案清單。',
    summaryEn: 'List project directories, workspace paths, and available repositories on the machine.',
    triggersZh: ['這台機器有哪些專案', '列出所有專案', '有什麼 project 可以開'],
    triggersEn: ['What projects are on this machine?', 'List all projects'],
  },
  start_session: {
    titleZh: '建立新工作 Session',
    titleEn: 'Start New Session',
    category: 'session',
    summaryZh: '在指定專案與機器上，使用選定的助理模型建立並啟動一個全新的工作 Session。',
    summaryEn: 'Create and launch a brand new working session on a project and machine.',
    triggersZh: ['幫我開一個新任務', '在某專案開新 Session', '新建一個工作'],
    triggersEn: ['Start a new session', 'Create a task in project X', 'Start a new worker'],
  },
  send_message_to_session: {
    titleZh: '發送文字訊息至 Session',
    titleEn: 'Send Message to Session',
    category: 'session',
    summaryZh: '將口語交代轉化為文字訊息或指令，直接發送至指定 Session 的終端執行或回答。',
    summaryEn: 'Send a text message or command directly to a session terminal or assistant.',
    triggersZh: ['幫我跟那個 Session 說……', '叫它先停下來', '跟它說繼續執行'],
    triggersEn: ['Tell that session to...', 'Tell it to stop', 'Send proceed to the task'],
  },
}

export function VoiceToolsCatalog() {
  const isZh = typeof document !== 'undefined' && document.documentElement.lang.startsWith('zh')
  const [filter, setFilter] = useState<ToolCategory>('all')
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  const categories: { key: ToolCategory; label: string }[] = [
    { key: 'all', label: W('voiceToolsCategoryAll') },
    { key: 'status', label: W('voiceToolsCategoryStatus') },
    { key: 'session', label: W('voiceToolsCategorySession') },
    { key: 'voice', label: W('voiceToolsCategoryVoice') },
    { key: 'tools', label: W('voiceToolsCategoryTools') },
  ]

  const toggleOne = (name: string) => {
    setExpanded((prev) => ({ ...prev, [name]: !prev[name] }))
  }

  const allNames = useMemo(() => COPILOT_TOOLS.map((t) => t.name), [])
  const allExpanded = allNames.length > 0 && allNames.every((name) => expanded[name])

  const toggleAll = () => {
    if (allExpanded) {
      setExpanded({})
    } else {
      const next: Record<string, boolean> = {}
      for (const name of allNames) next[name] = true
      setExpanded(next)
    }
  }

  const filteredTools = useMemo(() => {
    const q = search.trim().toLowerCase()
    return COPILOT_TOOLS.filter((tool) => {
      const meta = TOOL_METAS[tool.name]
      if (filter !== 'all' && meta?.category !== filter) return false
      if (!q) return true

      const nameMatch = tool.name.toLowerCase().includes(q)
      const descMatch = tool.description.toLowerCase().includes(q)
      const titleZhMatch = meta?.titleZh.toLowerCase().includes(q) ?? false
      const titleEnMatch = meta?.titleEn.toLowerCase().includes(q) ?? false
      const summaryZhMatch = meta?.summaryZh.toLowerCase().includes(q) ?? false
      const summaryEnMatch = meta?.summaryEn.toLowerCase().includes(q) ?? false
      const triggerMatch = [...(meta?.triggersZh ?? []), ...(meta?.triggersEn ?? [])].some((tr) =>
        tr.toLowerCase().includes(q),
      )
      const paramMatch = Object.keys(tool.parameters?.properties ?? {}).some((p) =>
        p.toLowerCase().includes(q),
      )

      return (
        nameMatch ||
        descMatch ||
        titleZhMatch ||
        titleEnMatch ||
        summaryZhMatch ||
        summaryEnMatch ||
        triggerMatch ||
        paramMatch
      )
    })
  }, [filter, search])

  return (
    <div className="voice-tools-catalog" aria-label={W('voiceTabTools')}>
      <div className="voice-tools-toolbar">
        <div className="voice-tools-categories" role="tablist" aria-label="Tool Categories">
          {categories.map((cat) => {
            const count = cat.key === 'all'
              ? COPILOT_TOOLS.length
              : COPILOT_TOOLS.filter((t) => TOOL_METAS[t.name]?.category === cat.key).length
            return (
              <button
                key={cat.key}
                type="button"
                role="tab"
                aria-selected={filter === cat.key}
                className={`voice-tools-cat-btn ${filter === cat.key ? 'active' : ''}`}
                onClick={() => setFilter(cat.key)}
              >
                <span>{cat.label}</span>
                <span className="voice-tools-cat-count">{count}</span>
              </button>
            )
          })}
        </div>

        <div className="voice-tools-search-row">
          <input
            type="search"
            className="voice-tools-search-input"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={W('voiceToolsSearchPlaceholder')}
            aria-label={W('voiceToolsSearchPlaceholder')}
          />
          <button
            type="button"
            className="voice-tools-toggle-all"
            onClick={toggleAll}
          >
            {allExpanded ? W('voiceToolsCollapseAll') : W('voiceToolsExpandAll')}
          </button>
        </div>
      </div>

      <div className="voice-tools-count-bar">
        <span>
          {W('voiceToolsCountSummary', {
            count: String(COPILOT_TOOLS.length),
            filtered: String(filteredTools.length),
          })}
        </span>
      </div>

      <div className="voice-tools-grid">
        {filteredTools.map((tool) => {
          const meta = TOOL_METAS[tool.name]
          const isOpened = !!expanded[tool.name]
          const paramsObj = tool.parameters as {
            properties?: Record<string, { type?: string; description?: string; enum?: string[] }>
            required?: readonly string[]
          } | undefined
          const props = paramsObj?.properties
          const propKeys = props ? Object.keys(props) : []
          const requiredKeys = (paramsObj?.required as readonly string[] | undefined) ?? []
          const title = isZh ? meta?.titleZh ?? tool.name : meta?.titleEn ?? tool.name
          const summary = isZh ? meta?.summaryZh : meta?.summaryEn
          const triggers = isZh ? meta?.triggersZh ?? [] : meta?.triggersEn ?? []
          const catLabel = categories.find((c) => c.key === meta?.category)?.label ?? meta?.category ?? ''

          return (
            <article
              key={tool.name}
              className={`voice-tool-card ${isOpened ? 'expanded' : ''}`}
            >
              <header className="voice-tool-header" onClick={() => toggleOne(tool.name)}>
                <div className="voice-tool-top-line">
                  <div className="voice-tool-labels">
                    <span className="voice-tool-cat-tag">{catLabel}</span>
                    <code className="voice-tool-code">{tool.name}</code>
                  </div>
                  <div className="voice-tool-status">
                    <span className="voice-tool-param-count">
                      {propKeys.length === 0
                        ? W('voiceToolsNoParamsCount')
                        : W('voiceToolsParamsCount', { count: String(propKeys.length) })}
                    </span>
                    <button
                      type="button"
                      className="voice-tool-toggle-btn"
                      aria-expanded={isOpened}
                      aria-label={isOpened ? W('voiceToolsCollapseAll') : W('voiceToolsExpandAll')}
                      onClick={(e) => {
                        e.stopPropagation()
                        toggleOne(tool.name)
                      }}
                    >
                      {isOpened ? '−' : '+'}
                    </button>
                  </div>
                </div>

                <h3 className="voice-tool-title">{title}</h3>

                {summary ? <p className="voice-tool-summary">{summary}</p> : null}

                {triggers.length > 0 && !isOpened ? (
                  <div className="voice-tool-sample-line">
                    <span className="voice-tool-sample-label">{W('voiceToolsTrySaying')}</span>
                    <span className="voice-tool-sample-text">{triggers[0]}</span>
                  </div>
                ) : null}
              </header>

              {isOpened ? (
                <div className="voice-tool-details">
                  <div className="voice-tool-section">
                    <div className="voice-tool-section-head">{W('voiceToolsSectionDesc')}</div>
                    <p className="voice-tool-full-desc">{tool.description}</p>
                  </div>

                  {triggers.length > 0 ? (
                    <div className="voice-tool-section">
                      <div className="voice-tool-section-head">{W('voiceToolsSectionTriggers')}</div>
                      <div className="voice-tool-triggers-list">
                        {triggers.map((tr) => (
                          <span key={tr} className="voice-tool-trigger-tag">
                            {tr}
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  <div className="voice-tool-section">
                    <div className="voice-tool-section-head">
                      {W('voiceToolsParameters')} ({propKeys.length})
                    </div>
                    {propKeys.length === 0 ? (
                      <p className="voice-tool-no-params">{W('voiceToolsNoParams')}</p>
                    ) : (
                      <div className="voice-tool-params-list">
                        {propKeys.map((k) => {
                          const p = props?.[k]
                          const isReq = requiredKeys.includes(k)
                          return (
                            <div key={k} className="voice-tool-param-row">
                              <div className="voice-tool-param-meta">
                                <code className="voice-tool-param-name">{k}</code>
                                <span className="voice-tool-param-type">{p?.type ?? 'any'}</span>
                                <span className={`voice-tool-param-req ${isReq ? 'required' : 'optional'}`}>
                                  {isReq ? W('voiceToolsRequired') : W('voiceToolsOptional')}
                                </span>
                              </div>
                              {p?.enum ? (
                                <div className="voice-tool-param-enum">
                                  <span className="voice-tool-enum-label">{W('voiceToolsEnumLabel')}</span>
                                  {p.enum.map((val) => (
                                    <code key={val} className="voice-tool-enum-val">{val}</code>
                                  ))}
                                </div>
                              ) : null}
                              {p?.description ? (
                                <p className="voice-tool-param-desc">{p.description}</p>
                              ) : null}
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                </div>
              ) : null}
            </article>
          )
        })}
      </div>
    </div>
  )
}
