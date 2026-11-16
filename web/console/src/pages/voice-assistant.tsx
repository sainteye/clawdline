import { useCallback, useEffect, useState } from 'react'
import type { PageModule } from './types.js'
import { nextWord as W } from '../next-strings.js'
import { CopilotSettingsCard } from './settings/window/CopilotSettingsCard.js'
import { VoiceToolsCatalog } from './voice-assistant-tools.js'
import { COPILOT_OPEN } from '../overlays/events.js'
import './voice-assistant.css'

export type VoiceAssistantTab = 'settings' | 'tools'

function parseTabFromHash(): VoiceAssistantTab {
  if (typeof window === 'undefined') return 'settings'
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  return params.get('tab') === 'tools' ? 'tools' : 'settings'
}

function VoiceAssistant({ shown }: { shown: boolean }) {
  const [tab, setTab] = useState<VoiceAssistantTab>(parseTabFromHash)

  useEffect(() => {
    const onHashChange = () => {
      setTab(parseTabFromHash())
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const switchTab = useCallback((nextTab: VoiceAssistantTab) => {
    setTab(nextTab)
    const params = new URLSearchParams(window.location.hash.replace(/^#/, ''))
    params.set('page', 'voice-assistant')
    params.set('tab', nextTab)
    window.location.hash = params.toString()
  }, [])

  return (
    <section className="page voice-work-page" hidden={!shown} aria-labelledby="voice-work-heading">
      <header className="voice-work-header">
        <div>
          <h1 id="voice-work-heading">{W('voiceWorkTitle')}</h1>
          <p>{tab === 'tools' ? W('voiceToolsCardJumpHint') : W('copilotSettingsHint')}</p>
        </div>
        <button type="button" onClick={() => document.dispatchEvent(new CustomEvent(COPILOT_OPEN))}>
          {W('voiceWorkCall')}
        </button>
      </header>

      <div className="voice-work-tabs" role="tablist" aria-label={W('voiceWorkTitle')}>
        <button
          type="button"
          role="tab"
          id="voice-tab-settings"
          aria-selected={tab === 'settings'}
          aria-controls="voice-pane-settings"
          className={`voice-work-tab-btn ${tab === 'settings' ? 'active' : ''}`}
          onClick={() => switchTab('settings')}
        >
          <span>⚙️</span>
          <span>{W('voiceTabSettings')}</span>
        </button>
        <button
          type="button"
          role="tab"
          id="voice-tab-tools"
          aria-selected={tab === 'tools'}
          aria-controls="voice-pane-tools"
          className={`voice-work-tab-btn ${tab === 'tools' ? 'active' : ''}`}
          onClick={() => switchTab('tools')}
        >
          <span>⚡</span>
          <span>{W('voiceTabTools')}</span>
          <span className="voice-work-tab-badge">27</span>
        </button>
      </div>

      <div
        id="voice-pane-settings"
        role="tabpanel"
        aria-labelledby="voice-tab-settings"
        hidden={tab !== 'settings'}
        className="voice-tab-pane"
      >
        <div className="sw-window sw-voice voice-work-settings">
          <CopilotSettingsCard shown={shown && tab === 'settings'} />
        </div>
      </div>

      <div
        id="voice-pane-tools"
        role="tabpanel"
        aria-labelledby="voice-tab-tools"
        hidden={tab !== 'tools'}
        className="voice-tab-pane"
      >
        <VoiceToolsCatalog />
      </div>
    </section>
  )
}

export const page: PageModule = { id: 'voice-assistant', requiresApiLevel: 56, Component: VoiceAssistant }
