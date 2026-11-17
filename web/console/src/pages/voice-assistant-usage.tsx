import {useCallback,useEffect,useRef,useState} from 'react'
import {onMachineChanged} from '../cloud/machine-switch.js'
import {nextWord} from '../next-strings.js'
import {formatUSD} from '../realtime/copilot-cost.js'
import {readCopilotSettings,type CopilotSettingsSnapshot} from './settings/api.js'

/** A separate page panel; entering it never starts a voice or model request. */
export function VoiceUsage({shown}:{shown:boolean}) {
 const [history,setHistory]=useState<CopilotSettingsSnapshot['usage']>()
 const [failure,setFailure]=useState(false)
 const [loading,setLoading]=useState(true)
 const generation=useRef(0)
 const read=useCallback(()=>{
  const serial=++generation.current
  setLoading(true);setFailure(false);setHistory(undefined)
  void readCopilotSettings().then(answer=>{if(serial===generation.current)setHistory(answer.usage)},()=>{if(serial===generation.current)setFailure(true)}).finally(()=>{if(serial===generation.current)setLoading(false)})
 },[])
 useEffect(()=>{if(shown)read();return()=>{generation.current++}},[shown,read])
 useEffect(()=>onMachineChanged(()=>{if(shown)read();else{generation.current++;setHistory(undefined);setLoading(true)}}),[shown,read])
 return (
      <section className="voice-usage-history" aria-busy={loading}>
        <h2>{nextWord("copilotDailyTitle")}</h2>
        <p className="sw-hint">{nextWord("copilotDailyHint")}</p>
        <button className="voice-work-call-btn" disabled={loading} onClick={read}>{nextWord(loading?"copilotDailyLoading":"copilotDailyRefresh")}</button>
        {failure || history?.error ? <p className="sw-said" role="alert">{nextWord("copilotDailyFailure")}</p> : null}
        {!loading && history && !history.days.length && !history.error ? <p className="sw-hint">{nextWord("copilotDailyEmpty")}</p> : null}
        {!loading && !failure && !history ? <p className="sw-hint">{nextWord("copilotDailyUnavailable")}</p> : null}
        {history?.days.map(day => <details key={day.date} className="sw-copilot-usage-day" open={day === history?.days[0]}>
          <summary>{day.date} · {day.known > 0 || day.usd > 0 ? formatUSD(day.usd) : '—'}</summary>
          <p className="sw-hint">{nextWord("copilotDailyCoverage",{calls:day.calls,missing:day.missing,partial:day.partial})}</p>
          <dl className="sw-copilot-usage-parts">
            {Object.entries(day.parts).filter(([,usd])=>usd>0).sort((a,b)=>b[1]-a[1]).map(([part,usd])=><div key={part}><dt>{nextWord(({voice:'copilotDailyVoice',text_input:'copilotDailyInput',cached_input:'copilotDailyCached',cache_write:'copilotDailyWrite',text_output:'copilotDailyOutput',audio_input:'copilotDailyAudioInput',audio_output:'copilotDailyAudioOutput',image_input:'copilotDailyImage',analysis:'copilotDailyAnalysis'} as Record<string,Parameters<typeof nextWord>[0]>)[part] ?? 'copilotDailyOther')}</dt><dd>{formatUSD(usd)}</dd></div>)}
          </dl>
          {day.input_tokens+day.output_tokens>0?<p className="sw-hint">{nextWord("copilotDailyTokens",{input:day.input_tokens,output:day.output_tokens,cached:day.cached_tokens})}</p>:null}
          {day.seconds>0?<p className="sw-hint">{nextWord("copilotDailyMinutes",{minutes:(day.seconds/60).toFixed(2)})}</p>:null}
        </details>)}
        <p className="sw-hint">{nextWord("copilotDailyLimits")}{' '}<a href="https://developers.openai.com/api/docs/pricing" target="_blank" rel="noreferrer">{nextWord("copilotDailyRates")}</a></p>
      </section>

 )
}
