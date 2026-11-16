import React from 'react'
import { createRoot } from 'react-dom/client'
import '../../src/legacy/tokens.css'
import '../../src/legacy/base.css'
import '../../src/realtime/realtime.css'
import { activateCatalog } from '../../src/catalog.js'
import { loadStrings } from '../../src/legacy/bridge.js'
import { RealtimeCopilotClient } from '../../src/realtime/realtime-copilot.js'

// The real modal and menu with no-provider transport substitutes.
const lang = new URLSearchParams(location.search).get('lang') === 'en' ? 'en' : 'zh-Hant'
const words = await (await fetch(`/catalogs/${lang}.json`)).json()
activateCatalog(words, lang)
await loadStrings(async () => words)
const mode = new URLSearchParams(location.search).get('mode')
const state = { status: 'connected' as const, connectionReady: mode !== 'not-ready', speakingState: 'listening' as const, remembered: 0, logs: [], lastAssistantSpeech: lang === 'en' ? 'The Session is comparing options. Ask for a summary or an explanation.' : '這個 Session 正在比較不同方案。你可以問目前進度，也可以深入了解比較內容。' }
const targets = [{ id: 'fixture-one', label: lang === 'en' ? 'Compare model options' : '比較模型方案', conversation: 'fixture-conversation' }]
RealtimeCopilotClient.prototype.getState = () => state
RealtimeCopilotClient.prototype.subscribe = listener => { listener(state); return () => {} }
RealtimeCopilotClient.prototype.questionTargets = async () => {
  if (mode === 'directory-error') throw new Error(lang === 'en' ? 'Directory unavailable' : '無法讀取清單')
  return { targets, current: 'fixture-one' }
}
RealtimeCopilotClient.prototype.askQuestion = async (id, text, target) => {
  document.getElementById('fixture-result')!.textContent = JSON.stringify({ id, text, target, submitted: mode !== 'send-error' })
  if (mode === 'send-error') throw new Error(lang === 'en' ? 'Not sent; try again' : '尚未送出，請再試一次')
}
const { RealtimeCopilotModal } = await import('../../src/realtime/RealtimeCopilotModal.js')
createRoot(document.getElementById('root')!).render(<><output id="fixture-result" style={{ position: 'fixed', top: 0, zIndex: 10001, background: '#222', color: '#fff', fontSize: 12 }}></output><RealtimeCopilotModal open={true} onClose={() => {}} /></>)
