import { useEffect, useRef, useState } from 'react'
import type { PageModule } from './types.js'
import { nextWord as W } from '../next-strings.js'
import { CopilotSettingsCard } from './settings/window/CopilotSettingsCard.js'
import { voiceWork, receiveVoiceWork, retryVoicePlanning } from '../voice-work/runtime.js'
import { WORK_TEXT_RUNES, retryableTask, type VoiceTask } from '../voice-work/engine.js'
import { COPILOT_OPEN } from '../overlays/events.js'
import { readCopilotSettings, writeCopilotSettings } from './settings/api.js'
import { Switch } from './settings/window/controls.js'
import { onMachineChanged } from '../cloud/machine-switch.js'
import { getStoredOpenAIKey } from '../realtime/realtime-copilot.js'
import './voice-assistant.css'

const statusWord:Record<string, Parameters<typeof W>[0]>={queued:'voiceWorkQueued',running:'voiceWorkRunning',waiting_confirmation:'voiceWorkConfirmation',waiting_result:'voiceWorkWaitingResult',paused:'voiceWorkPaused',cancel_requested:'voiceWorkCancelRequested',cancelled:'voiceWorkCancelled',outcome_unknown:'voiceWorkUnknown',failed:'voiceWorkFailed',done:'voiceWorkDone'}
function approvedQuestion(task:VoiceTask):string {
  const question=task.args._voice_question as {question?:unknown;option?:unknown}|undefined
  return question&&typeof question==='object'?`${String(question.question??'')} · ${String(question.option??'')}`:''
}
function VoiceAssistant({shown}:{shown:boolean}) {
  const [,redraw]=useState(0)
  const [text,setText]=useState('')
  const [sending,setSending]=useState(false)
  const [error,setError]=useState('')
  const [tab,setTab]=useState(location.hash.includes('tab=settings')?'settings':'work')
  const [history,setHistory]=useState(false)
  const [mode,setMode]=useState<'loading'|'enabled'|'disabled'|'unknown'>('loading')
  const [savingMode,setSavingMode]=useState(false)
  const [modeError,setModeError]=useState('')
  const modeRevision=useRef(0)
  useEffect(()=>voiceWork.subscribe(()=>redraw(n=>n+1)),[])
  useEffect(()=>shown?voiceWork.activate():undefined,[shown])
  useEffect(()=>{
    if(!shown||tab!=='work')return
    let live=true
    const read=()=>{
      const current=++modeRevision.current;setMode('loading');setSavingMode(false);setModeError('')
      void readCopilotSettings().then(answer=>{
        if(!live||current!==modeRevision.current)return
        setMode(answer.work_enabled===true?'enabled':'disabled')
        const route=new URLSearchParams(location.hash.slice(1))
        if(!answer.key_set&&!getStoredOpenAIKey()&&!route.has('tab')){
          route.set('tab','settings')
          window.history.replaceState(window.history.state,'',`#${route}`)
          setTab('settings')
        }
      },()=>{if(live&&current===modeRevision.current)setMode('unknown')})
    }
    read();const stop=onMachineChanged(read)
    return()=>{live=false;modeRevision.current++;stop()}
  },[shown,tab])
  const changeMode=async(enabled:boolean)=>{
    if(savingMode||mode==='loading'||mode==='unknown')return
    const current=modeRevision.current;setSavingMode(true);setModeError('')
    try {
      const answer=await writeCopilotSettings({work_enabled:enabled})
      if(current===modeRevision.current)setMode(answer.work_enabled===true?'enabled':'disabled')
    } catch(e){
      if(current===modeRevision.current){setMode('unknown');setModeError(e instanceof Error?e.message:String(e))}
    } finally {if(current===modeRevision.current)setSavingMode(false)}
  }
  useEffect(()=>{const changed=()=>setTab(location.hash.includes('tab=settings')?'settings':'work');window.addEventListener('hashchange',changed);return()=>window.removeEventListener('hashchange',changed)},[])
  const change=async(task:VoiceTask,action:string)=>{
    setError('')
    try {await voiceWork.change({action,id:task.id,version:task.version})} catch(e){setError(e instanceof Error?e.message:String(e))}
  }
  const tasks=voiceWork.state?.tasks??[]
  const unfinished=tasks.filter(t=>t.status!=='done'&&t.status!=='cancelled')
  const pending=voiceWork.state?.inputs.filter(i=>!i.planned)??[]
  return <section className="page voice-work-page" hidden={!shown} aria-labelledby="voice-work-heading">
    <header className="voice-work-header"><div><h1 id="voice-work-heading">{W('voiceWorkTitle')}</h1><p>{W('voiceWorkHint')}</p></div>
      <button type="button" onClick={()=>document.dispatchEvent(new CustomEvent(COPILOT_OPEN))}>{W('voiceWorkCall')}</button>
    </header>
    <nav className="voice-work-tabs" aria-label={W('voiceWorkTitle')}>
      <a href="#page=voice-assistant&tab=work" aria-current={tab==='work'?'page':undefined}>{W('voiceWorkList')} · {unfinished.length}</a>
      <a href="#page=voice-assistant&tab=settings" aria-current={tab==='settings'?'page':undefined}>{W('voiceWorkSettings')}</a>
    </nav>
    <div hidden={tab!=='work'}>
      <section className="voice-work-mode" aria-labelledby="voice-work-mode-heading" aria-describedby="voice-work-mode-hint">
        <div className="voice-work-mode-row"><div>
          <h2 id="voice-work-mode-heading">{W('voiceWorkEnable')}</h2>
          <p role="status">{W(savingMode?'voiceWorkSaving':mode==='enabled'?'voiceWorkModeEnabled':mode==='disabled'?'voiceWorkModeDisabled':mode==='unknown'?'voiceWorkModeUnknown':'voiceWorkModeLoading')}</p>
        </div><Switch label={W('voiceWorkEnable')} on={mode==='enabled'} disabled={savingMode||mode==='loading'||mode==='unknown'} onChange={on=>{void changeMode(on)}} /></div>
        <p id="voice-work-mode-hint" className="voice-work-field-hint">{W('voiceWorkInlineHint')}</p>
        {mode==='unknown'?<a href="#page=voice-assistant&tab=settings">{W('voiceWorkSettings')}</a>:null}
        {modeError?<p className="voice-work-error" role="alert"><strong>{W('voiceWorkIssue')}</strong>{W('voiceWorkFailure',{reason:modeError})}</p>:null}
      </section>
      <aside className="voice-work-notice" role="note"><span className="voice-work-notice-icon" aria-hidden="true">i</span><div><strong>{W('voiceWorkRuntimeLabel')}</strong><p>{W('voiceWorkRuntimeHint')}</p></div></aside>
      <form className="voice-work-compose" onSubmit={e=>{e.preventDefault();if(!text.trim()||sending)return;setSending(true);setError('');void receiveVoiceWork(text.trim()).then(()=>setText(''),e=>setError(String(e))).finally(()=>setSending(false))}}>
        <label htmlFor="voice-work-input">{W('voiceWorkInput')}</label>
        <textarea id="voice-work-input" rows={3} maxLength={WORK_TEXT_RUNES} value={text} onChange={e=>setText(e.target.value)} placeholder={W('voiceWorkInputPlaceholder')} aria-describedby="voice-work-example" />
        <p id="voice-work-example" className="voice-work-field-hint">{W('voiceWorkPlaceholder')}</p>
        <button type="submit" disabled={sending||!text.trim()}>{W(sending?'voiceWorkSaving':'voiceWorkReceive')}</button>
      </form>
      {voiceWork.state?.reply?<p className="voice-work-reply" role="status">{voiceWork.state.reply}</p>:null}
      {pending.length?<section className="voice-work-inbox"><h2>{W('voiceWorkInbox')} · {pending.length}</h2><ul>{pending.map(i=><li key={i.id}>{i.text}</li>)}</ul>
        <button type="button" disabled={voiceWork.planning} onClick={()=>{setError('');void retryVoicePlanning(pending.map(i=>i.id)).catch(e=>setError(String(e)))}}>{W(voiceWork.planning?'voiceWorkPlanning':'voiceWorkPlan')}</button>
        <p>{W('voiceWorkEnableReminder')}</p>
      </section>:null}
      {voiceWork.state?.planner_error?<p className="voice-work-error" role="alert"><strong>{W('voiceWorkIssue')}</strong>{W('voiceWorkPlannerFailed',{reason:voiceWork.state.planner_error})}</p>:null}
      {error||voiceWork.failure?<p className="voice-work-error" role="alert"><strong>{W('voiceWorkIssue')}</strong>{W('voiceWorkFailure',{reason:error||voiceWork.failure})}</p>:null}
      {!voiceWork.state&&!voiceWork.failure?<p role="status">{W('voiceWorkLoading')}</p>:null}
      {voiceWork.state&&!unfinished.length?<p className="voice-work-empty">{W('voiceWorkEmpty')}</p>:null}
      <div className="voice-work-tasks">{(history?tasks:unfinished).map(task=><article key={task.id} className="voice-work-task">
        <div className="voice-work-task-title"><h2>{task.text}</h2><span>{W(statusWord[task.status]??'voiceWorkUnknown')}</span></div>
        {task.depends_on.length?<p>{W('voiceWorkDepends',{tasks:task.depends_on.map(id=>tasks.find(t=>t.id===id)?.text??id).join(' · ')})}</p>:null}
        <details open={task.status==='waiting_confirmation'}><summary>{W('voiceWorkDetails')}</summary>
          <code>{task.tool}</code>{approvedQuestion(task)?<p>{approvedQuestion(task)}</p>:null}<pre>{JSON.stringify(Object.fromEntries(Object.entries(task.args).filter(([k])=>!k.startsWith('_voice_'))),null,2)}</pre>
          {task.result?<pre>{task.result}</pre>:null}{task.error?<p role="alert">{task.error}</p>:null}
        </details>
        <div className="voice-work-actions">
          {task.status==='waiting_confirmation'?<button type="button" onClick={()=>{void change(task,'confirm')}}>{W('voiceWorkConfirm')}</button>:null}
          {retryableTask(task)?<button type="button" onClick={()=>{void change(task,'resume')}}>{W(task.status==='outcome_unknown'?'voiceWorkRetryRead':'voiceWorkResume')}</button>:null}
          {['queued','running'].includes(task.status)?<button type="button" onClick={()=>{void change(task,'pause')}}>{W('voiceWorkPause')}</button>:null}
          {['waiting_result','outcome_unknown'].includes(task.status)?<button type="button" onClick={()=>{void change(task,'complete')}}>{W('voiceWorkComplete')}</button>:null}
          {!['done','cancelled','cancel_requested','outcome_unknown'].includes(task.status)?<button type="button" onClick={()=>{void change(task,'cancel')}}>{W('voiceWorkCancel')}</button>:null}
        </div>
      </article>)}</div>
      <label className="voice-work-history"><input type="checkbox" checked={history} onChange={e=>setHistory(e.target.checked)} />{W('voiceWorkHistory')}</label>
    </div>
    <div className="sw-window sw-voice voice-work-settings" hidden={tab!=='settings'}><p>{W('voiceWorkSettingsMoved')}</p><CopilotSettingsCard shown={shown&&tab==='settings'} /></div>
  </section>
}
export const page:PageModule={id:'voice-assistant',requiresApiLevel:56,Component:VoiceAssistant}
