import type { PageModule } from './types.js'
import { nextWord as W } from '../next-strings.js'
import { CopilotSettingsCard } from './settings/window/CopilotSettingsCard.js'
import { COPILOT_OPEN } from '../overlays/events.js'
import './voice-assistant.css'

function VoiceAssistant({ shown }: { shown: boolean }) {
  return (
    <section className="page voice-work-page" hidden={!shown} aria-labelledby="voice-work-heading">
      <header className="voice-work-header">
        <div>
          <h1 id="voice-work-heading">{W('voiceWorkTitle')}</h1>
          <p>{W('copilotSettingsHint')}</p>
        </div>
        <button type="button" onClick={() => document.dispatchEvent(new CustomEvent(COPILOT_OPEN))}>
          {W('voiceWorkCall')}
        </button>
      </header>
      <div className="sw-window sw-voice voice-work-settings">
        <CopilotSettingsCard shown={shown} />
      </div>
    </section>
  )
}

export const page: PageModule = { id: 'voice-assistant', requiresApiLevel: 56, Component: VoiceAssistant }
