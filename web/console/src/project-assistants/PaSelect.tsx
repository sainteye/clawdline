import { useEffect, useLayoutEffect, useId, useRef, useState, type ReactNode } from "react"

export interface PaOption { value:string; label:string; icon?:ReactNode; hint?:string; disabled?:boolean }
/** A line at the end of the list about the options themselves: still loading, failed (with a retry), or none to offer. */
export interface PaSelectStatus { kind:"loading"|"error"|"empty"; text:string; retryLabel?:string; onRetry?:()=>void }

export function SelectChevron({size=14}:{size?:number}) {
  return <svg className="pa-select-chevron" viewBox="0 0 16 16" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 6l4 4 4-4"/></svg>
}

/**
 * The Bots drop-down: a button that shows its value with a chevron, so it reads
 * as something to open, and a list drawn in this module's style instead of the
 * platform's. Keys: Enter, Space or the arrows open it; the arrows, Home and End
 * move; Enter or Space choose; Escape and Tab close. Escape is answered here so
 * it never also closes the panel the list sits in. A status retry is the last row the
 * arrows reach; choosing it retries and keeps the list open. The list opens upward when
 * the box it scrolls in has no room below.
 */
export function PaSelect({label,ariaLabel,value,options,onChange,placeholder="",disabled=false,busy=false,compact=false,status,children}:{
  label?:string;ariaLabel?:string;value:string;options:readonly PaOption[];onChange:(value:string)=>void;placeholder?:string;disabled?:boolean;busy?:boolean;compact?:boolean;status?:PaSelectStatus;children?:ReactNode
}) {
  const id=useId(),[open,setOpen]=useState(false),[active,setActive]=useState(0),[up,setUp]=useState(false)
  const root=useRef<HTMLDivElement>(null),button=useRef<HTMLButtonElement>(null),list=useRef<HTMLUListElement>(null)
  const current=options.find(o=>o.value===value)
  const retry=status?.onRetry?options.length:-1,rows=options.length+(retry>=0?1:0)
  const show=(at=Math.max(0,options.findIndex(o=>o.value===value)))=>{if(disabled||!rows)return;setActive(at);setOpen(true)}
  const close=(focus=true)=>{setOpen(false);if(focus)button.current?.focus({preventScroll:true})}
  const choose=(index:number)=>{
    const option=options[index]
    if(option?.disabled||button.current?.matches(":disabled"))return
    if(index===retry){status?.onRetry?.();return}
    if(option)onChange(option.value)
    close()
  }
  useLayoutEffect(()=>{
    if(!open){setUp(false);return}
    const node=list.current,anchor=button.current
    if(!node||!anchor)return
    let box:DOMRect={top:0,bottom:window.innerHeight} as DOMRect
    for(let e=anchor.parentElement;e;e=e.parentElement){const o=getComputedStyle(e).overflowY;if(o!=="visible"){box=e.getBoundingClientRect();break}}
    const a=anchor.getBoundingClientRect(),need=node.offsetHeight+6
    setUp(a.bottom+need>box.bottom&&a.top-need>=box.top)
  },[open,rows,status?.kind])
  const chooseRef=useRef(choose),activeRef=useRef(active);chooseRef.current=choose;activeRef.current=active
  useEffect(()=>{if(!open)return;list.current?.focus({preventScroll:true})},[open])
  useEffect(()=>{list.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView?.({block:"nearest"})},[active,open])
  useEffect(()=>{
    if(!open)return
    const away=(event:PointerEvent)=>{if(!root.current?.contains(event.target as Node))close(false)}
    // Native, so Escape stops here before the dialog's own listener closes the panel.
    const keys=(event:KeyboardEvent)=>{
      const last=rows-1
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
  },[open,options,rows])
  return <div className={`pa-select${compact?" pa-select-compact":""}`} ref={root} data-open={open||undefined} data-up={up||undefined}>
    {label&&<span className="pa-field-label" id={`${id}-label`}>{label}</span>}
    <div className="pa-select-anchor"><button type="button" ref={button} className="pa-select-button" disabled={disabled} aria-haspopup="listbox" aria-expanded={open} aria-busy={busy||undefined}
      aria-labelledby={label?`${id}-label ${id}-value`:undefined} aria-label={label?undefined:ariaLabel}
      onClick={()=>open?close():show()} onKeyDown={e=>{if(e.key==="ArrowDown"||e.key==="ArrowUp"){e.preventDefault();show()}}}>
      {current?.icon&&<span className="pa-select-icon">{current.icon}</span>}
      <span className={`pa-select-value${current?"":" pa-select-placeholder"}`} id={`${id}-value`}>{current?.label??placeholder}</span>
      {busy&&<span className="pa-select-spinner" aria-hidden="true"/>}
      <SelectChevron/>
    </button>
    {open&&<ul className="pa-select-list" role="listbox" ref={list} tabIndex={-1} aria-labelledby={label?`${id}-label`:undefined} aria-label={label?undefined:ariaLabel} aria-activedescendant={`${id}-${active}`}>
      {options.map((o,i)=><li key={o.value} id={`${id}-${i}`} data-index={i} role="option" aria-selected={o.value===value} aria-disabled={o.disabled||undefined} data-active={i===active||undefined}
        onPointerMove={()=>setActive(i)} onClick={()=>choose(i)}>
        {o.icon&&<span className="pa-select-icon">{o.icon}</span>}
        <span className="pa-select-text"><span>{o.label}</span>{o.hint&&<small>{o.hint}</small>}</span>
        {o.value===value&&<svg className="pa-select-check" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M3.5 8.5l3 3 6-7"/></svg>}
      </li>)}
      {status&&<li role="presentation" className="pa-select-status" data-kind={status.kind}>
        {status.kind==="loading"&&<span className="pa-select-spinner" aria-hidden="true"/>}
        <span role="status" aria-live="polite">{status.text}</span>
      </li>}
      {retry>=0&&<li id={`${id}-${retry}`} data-index={retry} role="option" aria-selected={false} className="pa-select-retry" data-active={active===retry||undefined}
        onPointerMove={()=>setActive(retry)} onClick={()=>choose(retry)}>
        <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M13 8a5 5 0 1 1-1.5-3.5M13 3v3h-3"/></svg>
        <span className="pa-select-text"><span>{status?.retryLabel}</span></span>
      </li>}
    </ul>}</div>
    {children}
  </div>
}

export { PaMenu, type PaMenuItem } from "./PaMenu"
