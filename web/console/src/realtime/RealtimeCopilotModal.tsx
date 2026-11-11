import React, { useState, useEffect } from 'react'
import {
  realtimeCopilot,
  getStoredOpenAIKey,
  setStoredOpenAIKey,
  type RealtimeCopilotState,
} from './realtime-copilot.js'

interface Props {
  open: boolean
  onClose: () => void
}

export const RealtimeCopilotModal: React.FC<Props> = ({ open, onClose }) => {
  const [copilotState, setCopilotState] = useState<RealtimeCopilotState>(realtimeCopilot.getState())
  const [apiKey, setApiKey] = useState(getStoredOpenAIKey())
  const [tempKey, setTempKey] = useState('')

  useEffect(() => {
    return realtimeCopilot.subscribe((state) => {
      setCopilotState(state)
    })
  }, [])

  useEffect(() => {
    if (open) {
      const stored = getStoredOpenAIKey()
      if (stored) {
        setApiKey(stored)
      }
    }
  }, [open])

  if (!open) return null

  const handleStartCall = () => {
    if (!apiKey.trim()) {
      return
    }
    setStoredOpenAIKey(apiKey)
    void realtimeCopilot.start(apiKey)
  }

  const handleHangup = () => {
    realtimeCopilot.disconnect()
    onClose()
  }

  const isConnected = copilotState.status === 'connected'
  const isConnecting = copilotState.status === 'connecting'

  let stateLabel = '準備通話'
  if (isConnecting) stateLabel = '正在建立 WebRTC 即時通話連線...'
  else if (isConnected) {
    if (copilotState.toolCallLog) stateLabel = copilotState.toolCallLog
    else if (copilotState.speakingState === 'speaking') stateLabel = '副駕駛正在回覆中...'
    else if (copilotState.speakingState === 'thinking') stateLabel = '正在思考與整理資料...'
    else stateLabel = '通話中 · 請開口說話（隨時可插話）'
  }

  return (
    <div className="realtime-modal-backdrop" onClick={(e) => {
      if (e.target === e.currentTarget && !isConnected && !isConnecting) onClose()
    }}>
      <div className="realtime-modal-sheet" role="dialog" aria-modal="true" aria-label="語音副駕駛即時通話">
        <div className="realtime-header">
          <div className="realtime-title-row">
            <span className="realtime-badge">Realtime Copilot</span>
            <button
              type="button"
              className="realtime-close-btn"
              onClick={handleHangup}
              aria-label="關閉"
            >
              ×
            </button>
          </div>
          <p className="realtime-desc">隨身語音副駕駛 · 掌握本地 Session 動態與即時指揮</p>
        </div>

        {/* Dynamic Voice Dot / Wave Visualizer */}
        <div className="realtime-visual-box">
          <div className={`realtime-orb ${isConnected ? `orb-${copilotState.speakingState}` : isConnecting ? 'orb-connecting' : 'orb-idle'}`}>
            <div className="orb-core"></div>
            <div className="orb-ring ring-1"></div>
            <div className="orb-ring ring-2"></div>
          </div>
          <div className="realtime-status-label">{stateLabel}</div>
        </div>

        {/* Live speech / transcript display */}
        {isConnected && (
          <div className="realtime-transcript-box">
            {copilotState.lastTranscript && (
              <div className="realtime-bubble user">
                <span className="bubble-label">你：</span>
                {copilotState.lastTranscript}
              </div>
            )}
            {copilotState.lastAssistantSpeech && (
              <div className="realtime-bubble assistant">
                <span className="bubble-label">副駕駛：</span>
                {copilotState.lastAssistantSpeech}
              </div>
            )}
          </div>
        )}

        {copilotState.error && (
          <div className="realtime-error-box" role="alert">
            {copilotState.error}
          </div>
        )}

        {/* Missing API Key Reminder Dialog */}
        {!isConnected && !apiKey && (
          <div className="realtime-key-reminder-box" role="alert">
            <div className="reminder-header">
              <span className="reminder-icon">🔑</span>
              <h4 className="reminder-title">尚未配置 OpenAI API 金鑰</h4>
            </div>
            <p className="reminder-desc">
              語音副駕駛需要 OpenAI Realtime API 金鑰以建立全雙工語音通話。您可前往「設定 &gt; 語音」完成設定，或直接在下方填入：
            </p>
            <div className="reminder-input-wrap">
              <input
                id="openai-key-input"
                type="password"
                placeholder="sk-..."
                value={tempKey}
                onChange={(e) => setTempKey(e.target.value)}
                autoComplete="off"
                spellCheck={false}
              />
              <button
                type="button"
                className="realtime-save-start-btn"
                disabled={!tempKey.trim()}
                onClick={() => {
                  const val = tempKey.trim()
                  if (!val) return
                  setStoredOpenAIKey(val)
                  setApiKey(val)
                  void realtimeCopilot.start(val)
                }}
              >
                儲存並開始通話
              </button>
            </div>
            <div className="reminder-footer">
              <button
                type="button"
                className="reminder-settings-link"
                onClick={() => {
                  onClose()
                  const row = document.querySelector<HTMLButtonElement>('#sidebar [data-page-to="settings"]')
                  if (row && !row.disabled) row.click()
                }}
              >
                前往「設定 &gt; 語音」設定金鑰 →
              </button>
            </div>
          </div>
        )}

        {/* Key configured indicator */}
        {!isConnected && apiKey && (
          <div className="realtime-key-status-row">
            <span className="realtime-key-ready">✓ 已載入 OpenAI API 金鑰</span>
            <button
              type="button"
              className="key-edit-link"
              onClick={() => {
                onClose()
                const row = document.querySelector<HTMLButtonElement>('#sidebar [data-page-to="settings"]')
                if (row && !row.disabled) row.click()
              }}
            >
              前往設定
            </button>
          </div>
        )}

        {/* Bottom Call Controls */}
        <div className="realtime-actions">
          {!isConnected && !isConnecting ? (
            apiKey ? (
              <button
                type="button"
                className="realtime-start-call-btn"
                onClick={handleStartCall}
              >
                <svg viewBox="0 0 24 24" className="call-ico" fill="currentColor">
                  <path d="M6.62 10.79a15.05 15.05 0 006.59 6.59l2.2-2.2a1 1 0 011.02-.24c1.12.37 2.33.57 3.57.57a1 1 0 011 1V20a1 1 0 01-1 1A17 17 0 013 4a1 1 0 011-1h3.5a1 1 0 011 1c0 1.25.2 2.45.57 3.57a1 1 0 01-.25 1.02l-2.2 2.2z"/>
                </svg>
                開始語音通話
              </button>
            ) : null
          ) : (
            <button
              type="button"
              className="realtime-hangup-btn"
              onClick={handleHangup}
            >
              <svg viewBox="0 0 24 24" className="call-ico" fill="currentColor">
                <path d="M12 9c-1.6 0-3.15.25-4.6.72v3.1c0 .39-.23.74-.58.9A14.88 14.88 0 012.05 15c-.58.12-1.05-.35-1.05-.94V10.5c0-.49.36-.9.84-.98 3.12-.5 6.34-.77 9.66-.77s6.54.27 9.66.77c.48.08.84.49.84.98v3.56c0 .59-.47 1.06-1.05.94a14.88 14.88 0 01-4.77-1.28.995.995 0 01-.58-.9v-3.1C15.15 9.25 13.6 9 12 9z"/>
              </svg>
              結束通話
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
