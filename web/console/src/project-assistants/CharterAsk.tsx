import { useEffect, useRef, useState } from "react"
import { askHeadings, insertAskRow, removeAskRow, shownAskRows, type AskList, type AskPolicy } from "./charter-ask"
import { charterFormat, charterWord } from "./charter-words"

// The charter's "what to ask you" section when it holds an ask policy: two
// columns (do without asking, always ask) above the exceptions and one line on
// how to ask; and its editor, where every item is its own row.

type Tone = AskList

/** The section's card body: its heading, the two columns, the exceptions and how to ask. */
export function AskPolicyView({ heading, policy }: { heading: string; policy: AskPolicy }) {
  const [free, ask, exceptions, how] = askHeadings[policy.language]
  return <div className="pa-charter-ask">
    <h2 className="pa-charter-ask-title">{heading}</h2>
    <div className="pa-charter-ask-columns">
      <AskViewList tone="free" heading={free} items={policy.free}/>
      <AskViewList tone="ask" heading={ask} items={policy.ask}/>
    </div>
    <AskViewList tone="exceptions" heading={exceptions} items={policy.exceptions}/>
    <p className="pa-charter-ask-how"><strong>{how}</strong><span>{policy.how || charterWord("askEmpty")}</span></p>
  </div>
}

function AskViewList({ tone, heading, items }: { tone: Tone; heading: string; items: readonly string[] }) {
  return <section className="pa-charter-ask-panel" data-tone={tone} aria-label={heading}>
    <h3><ToneIcon tone={tone}/><span>{heading}</span></h3>
    {items.length
      ? <ul>{items.map((item, i) => <li key={i}>{item}</li>)}</ul>
      : <p className="pa-charter-ask-empty">{charterWord("askEmpty")}</p>}
  </section>
}

/** The policy's rows: one input per item, a delete button each and an add button under each list. */
export function AskPolicyEditor({ policy, onChange }: { policy: AskPolicy; onChange: (policy: AskPolicy) => void }) {
  const [free, ask, exceptions, how] = askHeadings[policy.language]
  const set = (list: AskList) => (items: string[]) => onChange({ ...policy, [list]: items })
  return <div className="pa-charter-ask pa-charter-ask-editing">
    <div className="pa-charter-ask-columns">
      <AskRows tone="free" heading={free} items={policy.free} onChange={set("free")} first/>
      <AskRows tone="ask" heading={ask} items={policy.ask} onChange={set("ask")}/>
    </div>
    <AskRows tone="exceptions" heading={exceptions} items={policy.exceptions} onChange={set("exceptions")}/>
    <label className="pa-charter-ask-how pa-charter-ask-how-edit">
      <strong>{how}</strong>
      <input type="text" value={policy.how} onChange={event => onChange({ ...policy, how: event.target.value })}/>
    </label>
  </div>
}

function AskRows({ tone, heading, items, onChange, first }: { tone: Tone; heading: string; items: readonly string[]; onChange: (items: string[]) => void; first?: boolean }) {
  const inputs = useRef<(HTMLTextAreaElement | null)[]>([])
  const [focus, setFocus] = useState(first ? 0 : -1)
  useEffect(() => {
    if (focus < 0) return
    inputs.current[focus]?.focus({ preventScroll: false }); setFocus(-1)
  }, [focus, items])
  const rows = shownAskRows(items)
  const apply = (next: { items: string[]; focus: number }) => { onChange(next.items); setFocus(Math.max(next.focus, 0)) }
  return <section className="pa-charter-ask-panel" data-tone={tone} aria-label={heading}>
    <h3><ToneIcon tone={tone}/><span>{heading}</span></h3>
    <ol className="pa-charter-ask-rows">
      {rows.map((item, i) => <li key={i} className="pa-charter-ask-row">
        <span className="pa-charter-ask-number" aria-hidden="true">{i + 1}</span>
        <textarea ref={node => { inputs.current[i] = node; fitRow(node) }} rows={1} value={item} aria-label={charterFormat("askRow", heading, i + 1)}
          onChange={event => { fitRow(event.target); onChange(rows.map((row, j) => j === i ? event.target.value.replace(/\s*\n+\s*/g, " ") : row)) }}
          onKeyDown={event => {
            if (event.nativeEvent.isComposing) return
            if (event.key === "Enter") { event.preventDefault(); apply(insertAskRow(rows, i)) }
            else if (event.key === "Backspace" && item === "" && rows.length > 1) { event.preventDefault(); apply(removeAskRow(rows, i)) }
          }}/>
        <button type="button" className="pa-charter-ask-remove" aria-label={charterFormat("askRemove", heading, i + 1)} title={charterFormat("askRemove", heading, i + 1)}
          onClick={() => apply(removeAskRow(rows, i))}>×</button>
      </li>)}
    </ol>
    <button type="button" className="pa-charter-ask-add" onClick={() => { onChange([...rows, ""]); setFocus(rows.length) }}>{charterWord("askAdd")}</button>
  </section>
}

// A long item wraps inside its own row instead of scrolling out of sight, so
// every item reads whole while it is edited.
function fitRow(node: HTMLTextAreaElement | null) {
  if (!node) return
  node.style.height = "auto"
  node.style.height = `${node.scrollHeight + node.offsetHeight - node.clientHeight}px`
}

function ToneIcon({ tone }: { tone: Tone }) {
  if (tone === "exceptions") return null
  return <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {tone === "free"
      ? <><circle cx="8" cy="8" r="6.2"/><path d="M5.2 8.2 7.1 10l3.7-3.9"/></>
      : <><circle cx="8" cy="8" r="6.2"/><path d="M8 4.8v3.8"/><path d="M8 11.1v.1"/></>}
  </svg>
}
