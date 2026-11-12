import React, { useState, useEffect, useRef, useLayoutEffect } from 'react'
import { nextWord } from '../next-strings.js'
import { BRAND_MARK } from '../brand-mark.js'
import { formatUSD } from './copilot-cost.js'
import { CopilotOrb } from './CopilotOrb.js'
import { CopilotNotice } from './CopilotNotice.js'
import { CopilotDemoFilm } from './CopilotDemoFilm.js'
import {
  realtimeCopilot,
  type RealtimeCopilotState,
} from './realtime-copilot.js'

/** How each tool outcome is marked on its chip, and the word said beside it (`toolTrace`). */
const TOOL_OUTCOME_MARK = { done: '✓', asked: '?', many: '⋯', failed: '✗' } as const
const TOOL_OUTCOME_WORD = { done: 'copilotToolDone', asked: 'copilotToolAsked', many: 'copilotToolMany', failed: 'copilotToolFailed' } as const

/** One stable reader, so the orb is not woken by every render. */
const readCallLevels = () => realtimeCopilot.readLevels()

interface Props {
  open: boolean
  onClose: () => void
  /** Shrinks a live call to the floating bubble (`CopilotDockView`). */
  onMinimize?: () => void
}

export const RealtimeCopilotModal: React.FC<Props> = ({ open, onClose, onMinimize }) => {
  const [copilotState, setCopilotState] = useState<RealtimeCopilotState>(realtimeCopilot.getState())
  const [showLogs, setShowLogs] = useState(false)
  const [demoOpen, setDemoOpen] = useState(false)
  const [orbSize, setOrbSize] = useState(112)
  const drag = useRef<{ y: number; size: number } | null>(null)
  const transcript = useRef<HTMLDivElement>(null)
  const following = useRef(true)
  const resizeOrb = (size: number) => setOrbSize(Math.max(80, Math.min(220, size)))

  useLayoutEffect(() => {
    const box = transcript.current
    if (box && following.current) box.scrollTop = box.scrollHeight
  }, [copilotState.lastTranscript, copilotState.lastAssistantSpeech, open])

  useEffect(() => {
    return realtimeCopilot.subscribe((state) => {
      setCopilotState(state)
    })
  }, [])


  if (!open) return null

  const handleStartCall = () => {
    void realtimeCopilot.start()
  }

  const openSettings = () => {
    onClose()
    const row = document.querySelector<HTMLButtonElement>('#sidebar [data-page-to="settings"]')
    if (row && !row.disabled) row.click()
  }

  const handleHangup = () => {
    realtimeCopilot.disconnect()
    onClose()
  }

  const isConnected = copilotState.status === 'connected'
  const isConnecting = copilotState.status === 'connecting'

  let stateLabel = nextWord('copilotReady')
  if (isConnecting) stateLabel = nextWord('copilotConnecting')
  else if (isConnected) {
    if (copilotState.toolCallLog) stateLabel = copilotState.toolCallLog
    else if (copilotState.speakingState === 'speaking') stateLabel = nextWord('copilotAnswering')
    else if (copilotState.speakingState === 'thinking') stateLabel = nextWord('copilotThinking')
    else stateLabel = nextWord('copilotListening')
  }

  return (
    <div className="realtime-modal-backdrop" onClick={(e) => {
      if (e.target !== e.currentTarget) return
      // A tap beside a live call's sheet shrinks it; the call goes on.
      if (isConnected || isConnecting) onMinimize?.()
      else onClose()
    }}>
      <div className={`realtime-modal-sheet${isConnected ? " in-call" : ""}`} style={{ "--copilot-orb-size": `${orbSize}px` } as React.CSSProperties} role="dialog" aria-modal="true" aria-label={nextWord('copilotCallTitle')}>
        <div className="realtime-header">
          <div className="realtime-title-row">
            <span className="realtime-badge">Realtime Copilot</span>
            {(isConnected || isConnecting) && onMinimize ? (
              <button
                type="button"
                className="realtime-minimize-btn"
                onClick={onMinimize}
                aria-label={nextWord('copilotMinimize')}
                title={nextWord('copilotMinimize')}
              >
                <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="M3.5 8h9" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>
              </button>
            ) : null}
            <button
              type="button"
              className="realtime-close-btn"
              onClick={handleHangup}
              aria-label={nextWord('copilotClose')}
            >
              ×
            </button>
          </div>
          <p className="realtime-desc">{nextWord('copilotDesc')}</p>
        </div>

        {/* The cloud orb, moving with both voices */}
        <div className="realtime-visual-box">
          <div className="realtime-orb-resize" role="slider" tabIndex={0}
            aria-label={nextWord('copilotOrbResize')} aria-valuemin={80} aria-valuemax={220} aria-valuenow={orbSize}
            aria-orientation="vertical" title={nextWord('copilotOrbResize')}
            onPointerDown={(e) => {
              if (e.button !== 0) return
              drag.current = { y: e.clientY, size: orbSize }
              e.currentTarget.setPointerCapture(e.pointerId)
            }}
            onPointerMove={(e) => { if (drag.current) resizeOrb(drag.current.size + e.clientY - drag.current.y) }}
            onPointerUp={() => { drag.current = null }}
            onPointerCancel={() => { drag.current = null }}
            onLostPointerCapture={() => { drag.current = null }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowUp' || e.key === 'ArrowRight') { e.preventDefault(); resizeOrb(orbSize + 8) }
              else if (e.key === 'ArrowDown' || e.key === 'ArrowLeft') { e.preventDefault(); resizeOrb(orbSize - 8) }
              else if (e.key === 'Home') { e.preventDefault(); resizeOrb(80) }
              else if (e.key === 'End') { e.preventDefault(); resizeOrb(220) }
            }}>
            <CopilotOrb
              mode={isConnected ? copilotState.speakingState : isConnecting ? 'connecting' : 'idle'}
              levels={isConnected ? readCallLevels : undefined}
            />
            <span className="realtime-orb-grip" aria-hidden="true" />
          </div>
          <div className="realtime-status-label">{stateLabel}</div>
          <CopilotNotice notice={copilotState.notice} className="copilot-notice" />
        </div>

        {/* Live speech / transcript display */}
        {isConnected && (
          <div className="realtime-transcript-box" ref={transcript} role="region" tabIndex={0}
            aria-label={nextWord('copilotTranscript')}
            onScroll={(e) => { const box = e.currentTarget; following.current = box.scrollHeight - box.scrollTop - box.clientHeight < 24 }}>
            {!copilotState.lastTranscript && !copilotState.lastAssistantSpeech && <span className="realtime-transcript-empty">{nextWord('copilotTranscriptWaiting')}</span>}
            {copilotState.lastTranscript && (
              <div className="realtime-bubble user">
                <span className="bubble-label">{nextWord('copilotYouSaid')}</span>
                {copilotState.lastTranscript}
              </div>
            )}
            {copilotState.lastAssistantSpeech && (
              <div className="realtime-bubble assistant">
                <span className="bubble-label">
                  <svg className="bubble-mark" viewBox="0 0 16 11" role="img" aria-label="Clawdline" shapeRendering="crispEdges">
                    {BRAND_MARK.cells.flatMap((row, y) => row.map((color, x) => color &&
                      <rect key={`${x}-${y}`} x={x} y={y} width="1" height="1" fill={color} />))}
                  </svg>
                  {nextWord('copilotMarkSaid')}
                </span>
                {copilotState.lastAssistantSpeech}
              </div>
            )}
          </div>
        )}

        {isConnected && (
          <ol className="copilot-tools-trail" aria-label={nextWord('copilotToolsUsed')}>
            {(copilotState.tools ?? []).map((t, i) => (
              <li key={i} className={t.outcome} title={`${t.name}${t.target ? ` · ${t.target}` : ''} · ${nextWord(TOOL_OUTCOME_WORD[t.outcome])} · ${(t.ms / 1000).toFixed(1)} s`}>
                <span className="copilot-tool-mark" aria-hidden="true">{TOOL_OUTCOME_MARK[t.outcome]}</span>
                <code>{t.name}</code>
                {t.target ? <span className="copilot-tool-target">{t.target}</span> : null}
                {t.outcome === 'done' ? null : <span className="copilot-tool-outcome">{nextWord(TOOL_OUTCOME_WORD[t.outcome])}</span>}
                <span className="copilot-tool-ms">{(t.ms / 1000).toFixed(1)}s</span>
              </li>
            ))}
          </ol>
        )}

        <div className="realtime-secondary">
          {copilotState.cost && copilotState.cost.responses > 0 && (
            <details className="realtime-cost-row">
              <summary>
                <span className="realtime-cost-total">
                  {nextWord(copilotState.cost.priced ? 'copilotCost' : 'copilotCostUnpriced', {
                    cost: formatUSD(copilotState.cost.usd),
                    responses: copilotState.cost.responses,
                  })}
                </span>
                <span aria-hidden="true"> ⓘ</span>
              </summary>
              <span className="realtime-cost-note">{nextWord('copilotCostNote', { model: copilotState.cost.model })}</span>
            </details>
          )}

          {copilotState.remembered > 0 && (
            <div className="realtime-memory-row">
              <span>{nextWord('copilotRemembered', { count: copilotState.remembered })}</span>
              <button type="button" className="realtime-forget-btn" onClick={() => realtimeCopilot.forgetMemory()}>
                {nextWord('copilotForget')}
              </button>
            </div>
          )}

          {/* Realtime Event & Tool Call Logs */}
          {copilotState.logs && copilotState.logs.length > 0 && (
            <div className="realtime-logs-container">
              <button
                type="button"
                className="realtime-logs-toggle-btn"
                onClick={() => setShowLogs(!showLogs)}
              >
                <span>{nextWord(showLogs ? 'copilotLogsHide' : 'copilotLogsShow')}</span>
                <span className="logs-count">({copilotState.logs.length})</span>
              </button>
              {showLogs && (
                <div className="realtime-logs-list" role="log" aria-label={nextWord('copilotLogsLabel')}>
                  {copilotState.logs.map((item) => (
                    <div key={item.id} className={`log-item log-level-${item.level}`}>
                      <span className="log-time">[{item.timestamp}]</span>
                      <span className="log-tag">[{nextWord(item.level === 'tool' ? 'copilotLogTool' : item.level === 'error' ? 'copilotLogError' : 'copilotLogInfo')}]</span>
                      <span className="log-text">{item.text}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

        </div>

        {copilotState.error && (
          <div className="realtime-error-box" role="alert">
            {copilotState.error}
          </div>
        )}

        {copilotState.error && copilotState.error === nextWord('copilotErrorKeyMissing') && !isConnected ? (
          <div className="realtime-key-status-row">
            <button type="button" className="key-edit-link" onClick={openSettings}>
              {nextWord('copilotGoSettings')}
            </button>
            {/* Without a key there is no call yet: the film shows what one does. */}
            <button type="button" className="key-edit-link copilot-demo-watch" onClick={() => setDemoOpen(true)}>
              {nextWord('copilotDemoWatch')}
            </button>
          </div>
        ) : null}

        {/* Bottom Call Controls */}
        <div className="realtime-actions">
          {!isConnected && !isConnecting ? (
            (
              <button
                type="button"
                className="realtime-start-call-btn"
                onClick={handleStartCall}
              >
                <svg viewBox="0 0 24 24" className="call-ico" fill="currentColor">
                  <path d="M6.62 10.79a15.05 15.05 0 006.59 6.59l2.2-2.2a1 1 0 011.02-.24c1.12.37 2.33.57 3.57.57a1 1 0 011 1V20a1 1 0 01-1 1A17 17 0 013 4a1 1 0 011-1h3.5a1 1 0 011 1c0 1.25.2 2.45.57 3.57a1 1 0 01-.25 1.02l-2.2 2.2z"/>
                </svg>
                {nextWord('copilotStart')}
              </button>
            )
          ) : (
            <button
              type="button"
              className="realtime-hangup-btn"
              onClick={handleHangup}
            >
              <svg viewBox="0 0 24 24" className="call-ico" fill="currentColor">
                <path d="M12 9c-1.6 0-3.15.25-4.6.72v3.1c0 .39-.23.74-.58.9A14.88 14.88 0 012.05 15c-.58.12-1.05-.35-1.05-.94V10.5c0-.49.36-.9.84-.98 3.12-.5 6.34-.77 9.66-.77s6.54.27 9.66.77c.48.08.84.49.84.98v3.56c0 .59-.47 1.06-1.05.94a14.88 14.88 0 01-4.77-1.28.995.995 0 01-.58-.9v-3.1C15.15 9.25 13.6 9 12 9z"/>
              </svg>
              {nextWord('copilotHangup')}
            </button>
          )}
        </div>
      </div>
      <CopilotDemoFilm open={demoOpen} onClose={() => setDemoOpen(false)} />
    </div>
  )
}
