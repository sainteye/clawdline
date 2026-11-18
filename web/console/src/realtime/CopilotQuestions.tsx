import { ChevronIcon } from "../ui/ChevronIcon.js"
import React, { useEffect, useRef, useState } from 'react'
import { ProjectMark } from '../project-assistants/ProjectMark.js'
import { nextWord } from '../next-strings.js'
import { CALL_QUESTIONS, type CallQuestionID, type QuestionTarget } from './copilot-questions.js'

interface Props {
  compact?: boolean
  continueReport?: () => void
  ready: boolean
  targets: () => Promise<{ targets: QuestionTarget[]; current?: string }>
  send: (id: CallQuestionID, text: string, target: QuestionTarget) => Promise<void>
}

/** Complete questions teach the spoken vocabulary while remaining one-tap inputs. */
export function CopilotQuestions({ ready, targets, send, compact=false, continueReport }: Props) {
  const [open, setOpen] = useState(false)
  const [group, setGroup] = useState<'overview' | 'updates' | 'understand' | 'decisions'>('overview')
  const [rows, setRows] = useState<QuestionTarget[]>([])
  const [selected, setSelected] = useState('')
  const [choosing, setChoosing] = useState(false)
  const picker = useRef<HTMLDivElement>(null)
  const chooseButton = useRef<HTMLButtonElement>(null)
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  useEffect(() => {
    if (!open) return
    let current = true
    setChoosing(false)
    setLoading(true); setError(''); setRows([])
    void targets().then(result => {
      if (!current) return
      setRows(result.targets)
      setSelected(result.targets.some(row => row.id === result.current) ? result.current! : '')
    }).catch(error => { if (current) setError(String(error.message ?? error)) })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [open, targets])
  useEffect(() => {
    if (!choosing) return
    const outside = (event: PointerEvent) => { if (!picker.current?.contains(event.target as Node)) setChoosing(false) }
    document.addEventListener('pointerdown', outside)
    return () => document.removeEventListener('pointerdown', outside)
  }, [choosing])
  const picked = rows.find(row => row.id === selected)
  const ask = async (id: CallQuestionID, text: string) => {
    const target = rows.find(row => row.id === selected)
    if (!target || !ready || busy.current) return
    busy.current = true; setSending(true); setError('')
    try { await send(id, text, target); setOpen(false) }
    catch (error) { setError(String(error instanceof Error ? error.message : error)) }
    finally { busy.current = false; setSending(false) }
  }
  return <section className={`copilot-questions${compact ? " compact" : ""}`} onKeyDown={event => { if (event.key === 'Escape' && open) { event.stopPropagation(); if (choosing) { setChoosing(false); chooseButton.current?.focus(); return }; setOpen(false); (event.currentTarget.querySelector('.copilot-questions-toggle') as HTMLButtonElement | null)?.focus() } }}>
    <button type="button" className="copilot-questions-toggle" aria-expanded={open} aria-controls="copilot-questions-panel" onClick={() => setOpen(!open)}><span><QuestionIcon group="understand" />{nextWord('copilotQuestions')}</span>{!compact && <svg viewBox="0 0 20 20" aria-hidden="true"><path d={open ? 'm5 12 5-5 5 5' : 'm5 8 5 5 5-5'} /></svg>}</button>
    <div id="copilot-questions-panel" className={`copilot-questions-panel${open ? ' is-open' : ''}`} inert={!open} aria-hidden={!open}>
      <div className="copilot-questions-heading"><div><strong>{nextWord('copilotQuestions')}</strong><p>{nextWord('copilotQuestionsHint')}</p></div><button type="button" className="copilot-questions-close" aria-label={nextWord('copilotQuestionsClose')} onClick={() => setOpen(false)}>×</button></div>
      {continueReport && <button type="button" className="copilot-question-continue" onClick={() => {continueReport();setOpen(false)}}>{nextWord('copilotReportContinue')}</button>}
      <div className="copilot-questions-target" ref={picker}>
        <span id="copilot-question-target-label">{nextWord('copilotQuestionTarget')}</span>
        <button ref={chooseButton} type="button" className="copilot-target-toggle" aria-labelledby="copilot-question-target-label copilot-question-target-value" aria-expanded={choosing} aria-controls="copilot-target-options" disabled={loading || sending} onClick={() => setChoosing(!choosing)}>
          <span id="copilot-question-target-value">{picked ? <TargetName target={picked} /> : nextWord(loading ? 'copilotQuestionsLoading' : 'copilotQuestionChoose')}</span><span aria-hidden="true"><ChevronIcon /></span>
        </button>
        {choosing && <fieldset id="copilot-target-options" className="copilot-target-options"><legend>{nextWord('copilotQuestionChoose')}</legend>
          {rows.map(row => <label key={row.id} className={`copilot-target-option${selected === row.id ? ' selected' : ''}`}>
            <input type="radio" name="copilot-question-target" value={row.id} checked={selected === row.id} onChange={() => { setSelected(row.id); setChoosing(false); chooseButton.current?.focus() }} />
            <TargetName target={row} />
          </label>)}
        </fieldset>}
      </div>
      <div className="copilot-question-tabs" role="tablist" aria-label={nextWord('copilotQuestions')}>
        {GROUPS.map((item,index) => <button key={item} id={`copilot-question-tab-${item}`} type="button" role="tab" aria-selected={group === item} aria-controls="copilot-question-options" tabIndex={group === item ? 0 : -1} onClick={() => setGroup(item)} onKeyDown={event => {
          const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
          if (!step) return
          event.preventDefault(); const next = GROUPS[(index + step + GROUPS.length) % GROUPS.length]; setGroup(next); document.getElementById(`copilot-question-tab-${next}`)?.focus()
        }}><QuestionIcon group={item}/><span>{nextWord(GROUP_WORDS[item])}</span></button>)}
      </div>
      {!ready && <p className="copilot-questions-readiness">{nextWord('copilotQuestionNotReady')}</p>}
      <fieldset id="copilot-question-options" className="copilot-question-options" role="tabpanel" aria-labelledby={`copilot-question-tab-${group}`} disabled={!ready || loading || sending || !selected}>
        <legend className="copilot-question-legend">{nextWord(GROUP_WORDS[group])}</legend>
        {CALL_QUESTIONS.filter(question => question.group === group).map(question => <div className="copilot-question" key={question.id}>
          <button type="button" className="copilot-question-send" onClick={() => void ask(question.id, nextWord(question.key))}><span><strong>{nextWord(question.title)}</strong><span className="copilot-question-phrase">{nextWord(question.key)}</span></span><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 10h11m-5-5 5 5-5 5" /></svg></button>
          <details className="copilot-question-alternative"><summary><ChevronIcon direction="right" className="disclosure-chevron" />{nextWord('copilotQuestionAlternative')}</summary><button type="button" onClick={() => void ask(question.id, nextWord(question.alternate))}>{nextWord(question.alternate)}<span aria-hidden="true">→</span></button></details>
        </div>)}
      </fieldset>
      {sending && <p role="status">{nextWord('copilotQuestionSending')}</p>}
      {error && <p role="alert">{error}</p>}
    </div>
  </section>
}


const GROUPS = ['overview', 'updates', 'understand', 'decisions'] as const
const GROUP_WORDS = { overview: 'copilotQuestionsOverview', updates: 'copilotQuestionsUpdates', understand: 'copilotQuestionsUnderstand', decisions: 'copilotQuestionsDecisions' } as const
function QuestionIcon({group}:{group: typeof GROUPS[number]}) {
  const paths = {overview:'M4 5h12M4 10h8M4 15h10',updates:'M10 3a7 7 0 1 1-6 3M3 3v4h4m3-1v4l3 2',understand:'m10 2 8 5-8 5-8-5 8-5Zm-8 9 8 5 8-5m-16 4 8 5 8-5',decisions:'M6 6a4 4 0 0 1 8 0c0 3-4 3-4 6m0 3v1'}
  return <svg viewBox="0 0 20 20" aria-hidden="true"><path d={paths[group]}/></svg>
}

function TargetName({target}:{target:QuestionTarget}) {
  return <span className="copilot-target-name"><span className="copilot-target-icon" aria-hidden="true">{target.icon ? <ProjectMark icon={target.icon} size={24}/> : <svg viewBox="0 0 24 24"><path d="M3 6h7l2 2h9v12H3Z"/></svg>}</span><span><strong>{target.label}</strong>{(target.project || target.machineName) && <small>{[target.project, target.machineName].filter(Boolean).join(' · ')}</small>}</span></span>
}
