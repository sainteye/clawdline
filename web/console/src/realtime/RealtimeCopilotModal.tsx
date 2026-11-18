import { useModalBackdrop } from "../modal-backdrop.js"
import {CopilotToolHistory} from './CopilotToolHistory.js'
import { CopilotQuestions } from './CopilotQuestions.js'
import React, { useState, useEffect, useRef, useLayoutEffect } from 'react'
import { nextWord } from '../next-strings.js'
import { BRAND_IMAGE } from '../brand-mark.js'
import { formatDuration, formatUSD } from './copilot-cost.js'
import { CopilotOrb } from './CopilotOrb.js'
import {WORK_POLL_MS} from '../voice-work/engine.js'
import { CopilotNotice } from './CopilotNotice.js'
import { CopilotDemoFilm } from './CopilotDemoFilm.js'
import {
  realtimeCopilot,
  type RealtimeCopilotState,
} from './realtime-copilot.js'

/** One stable reader, so the orb is not woken by every render. */
const readCallLevels = () => realtimeCopilot.readLevels()
const questionTargets = () => realtimeCopilot.questionTargets()
const sendQuestion = realtimeCopilot.askQuestion.bind(realtimeCopilot)

interface Props {
  open: boolean
  onClose: () => void
  /** Shrinks a live call to the floating bubble (`CopilotDockView`). */
  onMinimize?: () => void
}

export const RealtimeCopilotModal: React.FC<Props> = ({ open, onClose, onMinimize }) => {
  const [copilotState, setCopilotState] = useState<RealtimeCopilotState>(realtimeCopilot.getState())
  const [now, setNow] = useState(Date.now)
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

  useEffect(() => {
    if (!open || copilotState.status !== 'connected' || copilotState.cost?.live) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), WORK_POLL_MS)
    return () => clearInterval(timer)
  }, [open, copilotState.status, copilotState.cost?.live])


  if (!open) return null

  const openSettings = () => {
    onClose()
    location.hash = "#page=voice-assistant&tab=settings"
  }

  const handleHangup = () => {
    realtimeCopilot.disconnect()
    onClose()
  }

  const isConnected = copilotState.status === 'connected'
  const isConnecting = copilotState.status === 'connecting'

  let stateLabel = nextWord('copilotReady')
  if (isConnecting) stateLabel = nextWord(CONNECTION_WORDS[copilotState.connectionPhase ?? 'settings'])
  else if (isConnected) {
    if (!copilotState.connectionReady) stateLabel=nextWord(CONNECTION_WORDS[copilotState.connectionPhase ?? 'configuration'])
    else if (copilotState.toolCallLog) stateLabel = copilotState.toolCallLog
    else if (copilotState.speakingState === 'speaking') stateLabel = nextWord('copilotAnswering')
    else if (copilotState.speakingState === 'thinking') stateLabel = nextWord('copilotThinking')
    else stateLabel = nextWord('copilotConnectedReady')
  }

  const backdrop = useModalBackdrop(() => {
    // A tap beside a live call's sheet shrinks it; the call goes on.
    if (isConnected || isConnecting) onMinimize?.()
    else onClose()
  })
  return (
    <div className="realtime-modal-backdrop" {...backdrop}>
      <div className={`realtime-modal-sheet${isConnected ? " in-call" : ""}`} style={{ "--copilot-orb-size": `${orbSize}px` } as React.CSSProperties} role="dialog" aria-modal="true" aria-label={nextWord('copilotCallTitle')}>
        <div className="realtime-content" role={isConnected?'region':undefined} aria-label={isConnected?nextWord('copilotCallContent'):undefined} tabIndex={isConnected?0:undefined}>
        <div className="realtime-header">
          <div className="realtime-title-row">
            {(isConnected || isConnecting) && copilotState.engine ? (
              // A ringing or live call can be placed again on the other voice system.
              <div className="realtime-engine-toggle" role="group" aria-label={nextWord('copilotEngineLabel')}>
                {(['live', 'realtime'] as const).map((engine) => (
                  <button key={engine} type="button" aria-pressed={copilotState.engine === engine}
                    className={copilotState.engine === engine ? 'on' : undefined}
                    onClick={() => realtimeCopilot.switchEngine(engine)}>
                    {nextWord(engine === 'live' ? 'copilotEngineLive' : 'copilotEngineRealtime')}
                  </button>
                ))}
              </div>
            ) : (
              <span className="realtime-badge">Realtime Copilot</span>
            )}
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
          {(!isConnected || !copilotState.connectionReady) && <div className="realtime-status-label">{stateLabel}</div>}
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
                  <img className="bubble-mark" src={BRAND_IMAGE} alt="Clawdline" />
                  {nextWord('copilotMarkSaid')}
                </span>
                {copilotState.lastAssistantSpeech}
              </div>
            )}
          </div>
        )}

        {(isConnected || isConnecting || !!copilotState.tools?.length) && <CopilotToolHistory records={copilotState.tools??[]} actions={
          <CopilotQuestions compact ready={isConnected && copilotState.connectionReady === true} targets={questionTargets} send={sendQuestion}
            continueReport={copilotState.sessionReportCanContinue ? () => realtimeCopilot.continueSessionReport() : undefined}/>
        }/>}

        <div className="realtime-secondary">
          {copilotState.cost && <div className="realtime-cost-row">
            <span className="realtime-cost-total">{nextWord(copilotState.cost.priced ? 'copilotCallUsage' : 'copilotCallUsageUnpriced', {
              duration: formatDuration(copilotState.cost.live?.seconds ?? (copilotState.startedAt ? Math.max(0, ((copilotState.endedAt ?? now) - copilotState.startedAt) / 1000) : 0)),
              cost: formatUSD(copilotState.cost.usd),
            })}</span>
          </div>}
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

        </div>

        {/* Bottom Call Controls */}
        <div className="realtime-actions">
          {!isConnected && !isConnecting ? (
              <button type="button" className="realtime-start-call-btn" onClick={() => { void realtimeCopilot.start() }}>
                <svg viewBox="0 0 24 24" className="call-ico" fill="currentColor">
                  <path d="M6.62 10.79a15.05 15.05 0 006.59 6.59l2.2-2.2a1 1 0 011.02-.24c1.12.37 2.33.57 3.57.57a1 1 0 011 1V20a1 1 0 01-1 1A17 17 0 013 4a1 1 0 011-1h3.5a1 1 0 011 1c0 1.25.2 2.45.57 3.57a1 1 0 01-.25 1.02l-2.2 2.2z"/>
                </svg>
                {nextWord('copilotStart')}
              </button>
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

const CONNECTION_WORDS = { settings: 'copilotConnectingSettings', capability: 'copilotConnectingCapability', directory: 'copilotConnectingDirectory', microphone: 'copilotConnectingMicrophone', offer: 'copilotConnectingOffer', ice: 'copilotConnectingICE', provider: 'copilotConnectingProvider', transport: 'copilotConnectingTransport', configuration: 'copilotConfiguring', ready: 'copilotConnectedReady' } as const
