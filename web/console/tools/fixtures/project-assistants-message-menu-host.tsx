/** The production window host and registry over the synthetic authenticated HTTP source. */
import React from "react"
import {createRoot} from "react-dom/client"
import {bootCatalog} from "../../src/catalog"
import {ProjectAssistantRegistry} from "../../src/cloud/project-assistants"
import {useProjectAssistantsHost} from "../../src/pages/project-assistants"
import "../../src/project-assistants/styles.css"

declare const TEMPLATE_MACHINE:string
const language=new URL(location.href).searchParams.get("lang")||"en"
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
const registry=new ProjectAssistantRegistry("synthetic-account","synthetic-reader")
registry.sync([{accountKey:registry.accountKey,readerID:registry.readerID,machineID:TEMPLATE_MACHINE,
 authorizationRevision:"synthetic-grant",apiLevel:89,paired:true,online:true,writable:true}],()=>(input,init)=>fetch(input,init))
function Host(){
 const host=useProjectAssistantsHost({registry,apiLevel:89,cloud:false,language})
 return <main><p>Synthetic origin view</p>{host.dock}{host.content}</main>
}
createRoot(document.getElementById("app")!).render(<React.StrictMode><Host/></React.StrictMode>)
