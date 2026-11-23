/** The real ImageViewer around a wide drawn picture with small print; no HTTP, source or model. */
import React,{useState} from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {ImageViewer} from "./ImageViewer"
import {words} from "./words"
import "./styles.css"

// A design board three times wider than a phone, its labels 9px high: legible only when zoomed in.
const canvas=document.createElement("canvas")
canvas.width=2400;canvas.height=1200
const ink=canvas.getContext("2d")!
ink.fillStyle="#f4f4f5";ink.fillRect(0,0,2400,1200)
for(let col=0;col<6;col++)for(let row=0;row<3;row++){
  const x=40+col*390,y=40+row*380
  ink.fillStyle=["#dbeafe","#dcfce7","#fef3c7"][row];ink.fillRect(x,y,350,340)
  ink.fillStyle="#18181b";ink.font="bold 22px system-ui";ink.fillText(`Screen ${row*6+col+1}`,x+16,y+36)
  ink.font="9px system-ui"
  for(let line=0;line<24;line++)ink.fillText(`Spacing 8px · label ${line+1} · token pa-${col}-${row}-${line}`,x+16,y+64+line*11)
}
const picture=await new Promise<Blob>(resolve=>canvas.toBlob(b=>resolve(b!),"image/png"))
const message={id:"m1",fence:{key:{machine_id:"fixture",bot_id:"bot"},conversation_id:"c1"},output:{id:"o1",name:"design-board.png"}} as unknown as W.ProjectAssistantMessage
const source={current:()=>true} as unknown as SourceClient
const language=new URL(location.href).searchParams.get("lang")??"en"
document.documentElement.lang=language
const state={closes:0}
Object.assign(window,{fixtureZoom:state})

function Fixture(){
  const [open,setOpen]=useState(true)
  return open?<ImageViewer message={message} source={source} w={words(language)} picture={picture} onClose={()=>{state.closes++;setOpen(false)}}/>:
    <button type="button" className="fixture-reopen" onClick={()=>setOpen(true)}>Reopen</button>
}
createRoot(document.querySelector("#app")!).render(<main className="project-assistants"><Fixture/></main>)
