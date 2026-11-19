import { ChevronIcon } from "../ui/ChevronIcon.js"
import {useState,type ReactNode} from "react"

/** Open secondary controls on demand, then retain their local state. */
export function SettingsDisclosure({title,summary,children,onOpen}:{title:string;summary?:string;children:ReactNode;onOpen?:()=>void}){
  const [visited,setVisited]=useState(false)
  return <details className="pa-settings-disclosure" onToggle={event=>{if(event.currentTarget.open){setVisited(true);onOpen?.()}}}>
    <summary><strong>{title}</strong>{summary&&<span>{summary}</span>}<ChevronIcon className="pa-settings-chevron" /></summary>
    {visited&&<div className="pa-settings-disclosure-body">{children}</div>}
  </details>
}
