import { useEffect, useRef } from "react"
import { createPortal } from "react-dom"
import { SidebarIcon, type SidebarIconName } from "../SidebarIcon.js"
import { appendToComposer } from "../legacy/snippets-bridge.js"
import "./clawdfather-suggestions.css"

type Suggestion = { title: string; prompt: string; icon: SidebarIconName }

const suggestions: Record<"zh" | "en", Suggestion[]> = {
  zh: [
    {
      title: "整理 Session 進度",
      icon: "verify",
      prompt: "請整理這台機器目前各 Session 的進度、待我決定的事項，以及無法確認的部分；附上觀察時間與來源。",
    },
    {
      title: "檢查機器狀況",
      icon: "devices",
      prompt: "請檢查 Clawdline 的機器負載與協調者狀態，回報需要處理的異常，並註明觀察時間與來源。",
    },
    {
      title: "檢視 Clawdline 設定",
      icon: "settings",
      prompt: "請列出目前可透過 Clawdline 調整的相關設定及其值，先說明建議與影響，不要直接修改。",
    },
    {
      title: "匯入或匯出專案設定",
      icon: "transfer",
      prompt: "請協助匯入或匯出指定專案的 Clawdline 設定與支援的助理設定。先問我要處理哪個專案，並列出範圍與衝突。",
    },
    {
      title: "建立看板項目並派工",
      icon: "work",
      prompt: "請先確認目標專案和工作需求，再建立看板項目並交給該專案的 Session；請勿自行修改程式碼。",
    },
  ],
  en: [
    {
      title: "Summarize Sessions",
      icon: "verify",
      prompt: "Summarize the progress of Sessions on this machine, decisions waiting for me, and unknowns. Cite the observation time and source.",
    },
    {
      title: "Check machine status",
      icon: "devices",
      prompt: "Check Clawdline machine load and coordinator status. Report any issue that needs attention with observation time and source.",
    },
    {
      title: "Review Clawdline settings",
      icon: "settings",
      prompt: "List relevant settings Clawdline can change and their current values. Explain your recommendation and its effect before changing anything.",
    },
    {
      title: "Import or export project settings",
      icon: "transfer",
      prompt: "Help import or export Clawdline settings and supported assistant settings for a project. Ask which project first, then list the scope and conflicts.",
    },
    {
      title: "Create a Board item and delegate",
      icon: "work",
      prompt: "Confirm the target project and work request, create a Board item, then assign it to a Session in that project. Do not edit source code yourself.",
    },
  ],
}

export function ClawdfatherSuggestions({ onClose }: { onClose: () => void }) {
  const first = useRef<HTMLButtonElement>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  const zh = /^zh/i.test(document.documentElement.lang || navigator.language)
  const rows = suggestions[zh ? "zh" : "en"]

  useEffect(() => {
    first.current?.focus({ preventScroll: true })
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault()
        event.stopImmediatePropagation()
        close()
      }
      if (event.key === "Tab") {
        const controls = [...document.querySelectorAll<HTMLElement>("#clawdfather-suggestions button")]
        const at = controls.indexOf(document.activeElement as HTMLElement)
        const next = (at + (event.shiftKey ? -1 : 1) + controls.length) % controls.length
        event.preventDefault()
        controls[next]?.focus()
      }
    }
    document.addEventListener("keydown", key, true)
    return () => document.removeEventListener("keydown", key, true)
  }, [])

  const close = () => {
    closeRef.current()
    document.getElementById("detail-snippets")?.focus({ preventScroll: true })
  }
  const pick = (prompt: string) => {
    closeRef.current()
    appendToComposer(prompt)
    document.getElementById("msg")?.focus({ preventScroll: true })
  }

  return createPortal(
    <div className="overlay" id="clawdfather-suggestions" onClick={close}>
      <div className="sheet clawdfather-suggestions" role="dialog" aria-modal="true"
        aria-labelledby="clawdfather-suggestions-title" onClick={(event) => event.stopPropagation()}>
        <div className="clawdfather-suggestions-head">
          <h2 id="clawdfather-suggestions-title">{zh ? "可以交給 Clawdfather" : "Ask Clawdfather"}</h2>
          <button type="button" className="clawdfather-suggestions-close" onClick={close}
            aria-label={zh ? "關閉" : "Close"}>×</button>
        </div>
        <div className="clawdfather-suggestions-list">
          {rows.map((item, index) => (
            <button key={item.title} type="button" ref={index === 0 ? first : undefined}
              onClick={() => pick(item.prompt)}>
              <SidebarIcon name={item.icon} />
              <span>{item.title}</span>
            </button>
          ))}
        </div>
      </div>
    </div>,
    document.body,
  )
}
