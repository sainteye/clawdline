import React, { useEffect, useRef, useState } from 'react'
import { nextWord } from '../next-strings.js'
import { CALL_QUESTIONS, type CallQuestionID, type QuestionTarget } from './copilot-questions.js'

interface Props {
  ready: boolean
  targets: () => Promise<{ targets: QuestionTarget[]; current?: string }>
  send: (id: CallQuestionID, text: string, target: QuestionTarget) => Promise<void>
}

/** Complete questions teach the spoken vocabulary while remaining one-tap inputs. */
export function CopilotQuestions({ ready, targets, send }: Props) {
  const [open, setOpen] = useState(false)
  const [rows, setRows] = useState<QuestionTarget[]>([])
  const [selected, setSelected] = useState('')
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  useEffect(() => {
    if (!open) return
    let current = true
    setLoading(true); setError(''); setRows([])
    void targets().then(result => {
      if (!current) return
      setRows(result.targets)
      setSelected(result.targets.some(row => row.id === result.current) ? result.current! : '')
    }).catch(error => { if (current) setError(String(error.message ?? error)) })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [open, targets])
  const ask = async (id: CallQuestionID, text: string) => {
    const target = rows.find(row => row.id === selected)
    if (!target || !ready || busy.current) return
    busy.current = true; setSending(true); setError('')
    try { await send(id, text, target); setOpen(false) }
    catch (error) { setError(String(error instanceof Error ? error.message : error)) }
    finally { busy.current = false; setSending(false) }
  }
  return <section className="copilot-questions" onKeyDown={event => { if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false); (event.currentTarget.querySelector('.copilot-questions-toggle') as HTMLButtonElement | null)?.focus() } }}>
    <button type="button" className="copilot-questions-toggle" aria-expanded={open} aria-controls="copilot-questions-panel" onClick={() => setOpen(!open)}>{nextWord('copilotQuestions')} <span aria-hidden="true">{open ? '▴' : '▾'}</span></button>
    {open && <div id="copilot-questions-panel" className="copilot-questions-panel">
      <p>{nextWord('copilotQuestionsHint')}</p>
      <label>{nextWord('copilotQuestionTarget')}<select value={selected} disabled={loading || sending} onChange={event => setSelected(event.target.value)}>
        <option value="">{nextWord(loading ? 'copilotQuestionsLoading' : 'copilotQuestionChoose')}</option>
        {rows.map(row => <option key={row.id} value={row.id}>{row.label}{row.machineName ? ` · ${row.machineName}` : ''}</option>)}
      </select></label>
      {!ready && <p>{nextWord('copilotQuestionNotReady')}</p>}
      {(['overview', 'updates', 'understand', 'decisions'] as const).map(group => <fieldset key={group} disabled={!ready || loading || sending || !selected}>
        <legend>{nextWord(({ overview: 'copilotQuestionsOverview', updates: 'copilotQuestionsUpdates', understand: 'copilotQuestionsUnderstand', decisions: 'copilotQuestionsDecisions' } as const)[group])}</legend>
        {CALL_QUESTIONS.filter(question => question.group === group).map(question => <div className="copilot-question" key={question.id}>
          <button type="button" onClick={() => void ask(question.id, nextWord(question.key))}>{nextWord(question.key)} <span aria-hidden="true">↗</span></button>
          <details><summary>{nextWord('copilotQuestionAlternative')}</summary><button type="button" onClick={() => void ask(question.id, nextWord(question.alternate))}>{nextWord(question.alternate)} <span aria-hidden="true">↗</span></button></details>
        </div>)}
      </fieldset>)}
      {sending && <p role="status">{nextWord('copilotQuestionSending')}</p>}
      {error && <p role="alert">{error}</p>}
    </div>}
  </section>
}
