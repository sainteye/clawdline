import {useState,type ReactNode} from "react"

/** Open secondary controls on demand, then retain their local state. */
export function SettingsDisclosure({title,summary,children}:{title:string;summary?:string;children:ReactNode}){
  const [visited,setVisited]=useState(false)
  return <details className="pa-settings-disclosure" onToggle={event=>{if(event.currentTarget.open)setVisited(true)}}>
    <summary><strong>{title}</strong>{summary&&<span>{summary}</span>}</summary>
    {visited&&<div className="pa-settings-disclosure-body">{children}</div>}
  </details>
}
