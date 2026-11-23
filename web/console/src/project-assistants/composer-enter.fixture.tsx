/** The real Bot composer with synthetic props, so Enter can be pressed without a daemon or provider. */
import React,{useState} from "react"
import {createRoot} from "react-dom/client"
import {Composer} from "./Composer"
import {words} from "./words"
import "./styles.css"
const params=new URL(location.href).searchParams
const record={sends:[] as string[],busy:false,blocked:params.has("blocked")}
;(window as unknown as {composer:typeof record}).composer=record
const mentions=[{id:"reviewer",name:"Reviewer",appearance:{shape:"blob",color:"#73cda5"},agent:"codex",project:"Fixture Project"}] as const
function App(){
 const [text,setText]=useState(""),[busy,setBusy]=useState(false)
 // A send is busy until the fixture releases it, as a real send waits for its receipt.
 const send=()=>{record.sends.push(text);record.busy=true;setBusy(true);setText("")}
 ;(window as unknown as {release:()=>void}).release=()=>{record.busy=false;setBusy(false)}
 return <main className="project-assistants" style={{padding:16}}>
  <Composer text={text} onText={setText} onSend={send} disabled={false} busy={busy} sendBlocked={record.blocked} onClearQuote={()=>{}} w={words("en")} mentions={mentions as never} plain/>
 </main>
}
createRoot(document.querySelector("#app")!).render(<App/>)
