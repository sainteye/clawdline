import { useEffect, useLayoutEffect, useId, useRef, useState, type ReactNode } from "react"

export interface PaOption { value:string; label:string; icon?:ReactNode; hint?:string }

const chevron=<svg className="pa-select-chevron" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 6l4 4 4-4"/></svg>

/**
 * The Bots drop-down: a button that shows its value with a chevron, so it reads
 * as something to open, and a list drawn in this module's style instead of the
 * platform's. Keys: Enter, Space or the arrows open it; the arrows, Home and End
 * move; Enter or Space choose; Escape and Tab close. Escape is answered here so
 * it never also closes the panel the list sits in.
 */
export function PaSelect({label,ariaLabel,value,options,onChange,placeholder="",disabled=false,busy=false,compact=false,children}:{
  label?:string;ariaLabel?:string;value:string;options:readonly PaOption[];onChange:(value:string)=>void;placeholder?:string;disabled?:boolean;busy?:boolean;compact?:boolean;children?:ReactNode
}) {
  const id=useId(),[open,setOpen]=useState(false),[active,setActive]=useState(0)
  const root=useRef<HTMLDivElement>(null),button=useRef<HTMLButtonElement>(null),list=useRef<HTMLUListElement>(null)
  const current=options.find(o=>o.value===value)
  const show=(at=Math.max(0,options.findIndex(o=>o.value===value)))=>{if(disabled||!options.length)return;setActive(at);setOpen(true)}
  const close=(focus=true)=>{setOpen(false);if(focus)button.current?.focus({preventScroll:true})}
  const choose=(index:number)=>{const option=options[index];if(option){onChange(option.value)}close()}
  const chooseRef=useRef(choose),activeRef=useRef(active);chooseRef.current=choose;activeRef.current=active
  useEffect(()=>{if(!open)return;list.current?.focus({preventScroll:true})},[open])
  useEffect(()=>{list.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView?.({block:"nearest"})},[active,open])
  useEffect(()=>{
    if(!open)return
    const away=(event:PointerEvent)=>{if(!root.current?.contains(event.target as Node))close(false)}
    // Native, so Escape stops here before the dialog's own listener closes the panel.
    const keys=(event:KeyboardEvent)=>{
      const last=options.length-1
      if(event.key==="Escape"){event.preventDefault();event.stopPropagation();close();return}
      if(event.key==="Tab"){close(false);return}
      if(event.key==="ArrowDown"){event.preventDefault();setActive(a=>Math.min(last,a+1))}
      else if(event.key==="ArrowUp"){event.preventDefault();setActive(a=>Math.max(0,a-1))}
      else if(event.key==="Home"){event.preventDefault();setActive(0)}
      else if(event.key==="End"){event.preventDefault();setActive(last)}
      else if(event.key==="Enter"||event.key===" "){event.preventDefault();event.stopPropagation();chooseRef.current(activeRef.current)}
    }
    const node=list.current
    document.addEventListener("pointerdown",away,true);node?.addEventListener("keydown",keys)
    return()=>{document.removeEventListener("pointerdown",away,true);node?.removeEventListener("keydown",keys)}
  },[open,options])
  return <div className={`pa-select${compact?" pa-select-compact":""}`} ref={root} data-open={open||undefined}>
    {label&&<span className="pa-field-label" id={`${id}-label`}>{label}</span>}
    <div className="pa-select-anchor"><button type="button" ref={button} className="pa-select-button" disabled={disabled} aria-haspopup="listbox" aria-expanded={open} aria-busy={busy||undefined}
      aria-labelledby={label?`${id}-label ${id}-value`:undefined} aria-label={label?undefined:ariaLabel}
      onClick={()=>open?close():show()} onKeyDown={e=>{if(e.key==="ArrowDown"||e.key==="ArrowUp"){e.preventDefault();show()}}}>
      {current?.icon&&<span className="pa-select-icon">{current.icon}</span>}
      <span className={`pa-select-value${current?"":" pa-select-placeholder"}`} id={`${id}-value`}>{current?.label??placeholder}</span>
      {chevron}
    </button>
    {open&&<ul className="pa-select-list" role="listbox" ref={list} tabIndex={-1} aria-labelledby={label?`${id}-label`:undefined} aria-label={label?undefined:ariaLabel} aria-activedescendant={`${id}-${active}`}>
      {options.map((o,i)=><li key={o.value} id={`${id}-${i}`} data-index={i} role="option" aria-selected={o.value===value} data-active={i===active||undefined}
        onPointerMove={()=>setActive(i)} onClick={()=>choose(i)}>
        {o.icon&&<span className="pa-select-icon">{o.icon}</span>}
        <span className="pa-select-text"><span>{o.label}</span>{o.hint&&<small>{o.hint}</small>}</span>
        {o.value===value&&<svg className="pa-select-check" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M3.5 8.5l3 3 6-7"/></svg>}
      </li>)}
    </ul>}</div>
    {children}
  </div>
}

export { PaMenu, type PaMenuItem } from "./PaMenu"
