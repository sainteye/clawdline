import { useEffect, useRef, useState } from "react"
import { nextWord } from "../../next-strings.js"
import { InstaBoxKey } from "./InstaBoxWake.js"
import type { CloudMachine } from "../../cloud/copied.js"

export function CloudMachineSetup({ apiOrigin, machines, refresh, pair, start, close }: {
  apiOrigin: string
  machines: readonly CloudMachine[]
  refresh: () => Promise<boolean>
  pair: (id: string, approvedName?: string) => void
  start: () => boolean
  close: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [started, setStarted] = useState(false)
  const [failed, setFailed] = useState(false)
  const [approved, setApproved] = useState<{ id: string; name: string } | null>(null)
  const paired = approved && machines.some(row => row.id === approved.id && row.pairing === "paired")
  useEffect(() => { dialog.current?.showModal() }, [])

  return <dialog ref={dialog} className="am-cloud-setup" aria-labelledby="am-cloud-setup-title" onClose={close}>
    <header className="am-cloud-setup-head">
      <h2 id="am-cloud-setup-title">{nextWord("msSetupCloud")}</h2>
      <button className="am-ghost" type="button" onClick={() => dialog.current?.close()}>{nextWord("msSetupClose")}</button>
    </header>
    <div className="am-cloud-setup-body">
      <article className="am-cloud-offer">
        <div className="am-cloud-offer-head">
          <span className="am-cloud-offer-icon" aria-hidden="true">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
              <path d="M4 15a4 4 0 0 1 4-4h.2a5 5 0 0 1 9.7 1A3.5 3.5 0 0 1 17 19H8a4 4 0 0 1-4-4Z"/>
              <path d="m12 13 0 4m-2-2 4 0"/>
            </svg>
          </span>
          <div><h3>InstaBox</h3><p>{nextWord("msInstaFreeSummary")}</p></div>
          <a href="https://instabox.sh/pricing" target="_blank" rel="noopener noreferrer">{nextWord("msSetupPricing")}</a>
        </div>
        <p>{nextWord("msInstaSessionHelp")}</p>
      </article>
      <InstaBoxKey origin={apiOrigin} />
      <details className="am-cloud-option">
        <summary>{nextWord("msInstaApproval")}</summary>
        <div className="am-stack">
          <CloudMachineEnrollment apiOrigin={apiOrigin} onRegistered={(id, name) => { setApproved({ id, name }); void refresh() }} />
          {approved && <p className="am-say"><strong>{approved.name}</strong></p>}
          {approved && !paired && <button type="button" className="am-ghost" onClick={() => pair(approved.id, approved.name)}>{nextWord("msPair")}</button>}
        </div>
      </details>
      {failed && <p className="am-say" role="alert">{nextWord("msInstaNoHelper")}</p>}
    </div>
    <footer className="am-cloud-setup-foot">
      <button type="button" className="am-accent" disabled={started} onClick={() => {
        if (start()) { setStarted(true); dialog.current?.close() }
        else setFailed(true)
      }}>{nextWord("msInstaLaunch")}</button>
    </footer>
  </dialog>
}

type MachineReview = { name: string; fingerprint: string; review_digest: string }
const deviceCode = /^[A-Z0-9]{4}-?[A-Z0-9]{4}$/

function CloudMachineEnrollment({apiOrigin,onRegistered}:{apiOrigin:string;onRegistered:(id:string,name:string)=>void}) {
  const [code,setCode]=useState("")
  const [review,setReview]=useState<MachineReview|null>(null)
  const [state,setState]=useState<"idle"|"busy"|"unavailable"|"approved">("idle")
  const pending=useRef(false)

  const submit=async()=>{
    if(pending.current||!deviceCode.test(code))return
    pending.current=true;setState("busy")
    try {
      const action=review?"approve":"preview"
      const response=await fetch(new URL("/v1/auth/device/"+action,apiOrigin),{
        method:"POST",credentials:"include",cache:"no-store",headers:{"content-type":"application/json"},
        body:JSON.stringify(review?{user_code:code,expected_kind:"machine",review_digest:review.review_digest}:{user_code:code}),
      })
      if(!response.ok)throw new Error("Device authorization refused")
      const result=await response.json()
      if(review){
        if(typeof result.machine_id!=="string"||!result.machine_id||result.machine_name!==review.name)throw new Error("Unrecognized approval receipt")
        setCode("");setReview(null);setState("approved");onRegistered(result.machine_id, review.name)
      }else{
        if(result.kind!=="machine"||typeof result.name!=="string"||!result.name||
          typeof result.fingerprint!=="string"||!result.fingerprint||
          typeof result.review_digest!=="string"||!/^[a-f0-9]{64}$/.test(result.review_digest))throw new Error("Machine review required")
        setReview({name:result.name,fingerprint:result.fingerprint,review_digest:result.review_digest});setState("idle")
      }
    }catch{
      setState("unavailable")
    }finally{
      pending.current=false
    }
  }
  return <form className="am-cloud-enrollment" onSubmit={event=>{event.preventDefault();void submit()}}>
    {review?<p><strong>{review.name}</strong><br/><small>{nextWord("msSetupFingerprint",{fingerprint:review.fingerprint})}</small></p>:
      <label>{nextWord("msSetupEnrollmentCode")}
        <input type="password" autoComplete="one-time-code" autoCapitalize="characters" spellCheck={false}
          placeholder="ABCD-EFGH" maxLength={9} disabled={state==="busy"}
          value={code} onChange={event=>{setCode(event.currentTarget.value.trim().toUpperCase());setState("idle")}} />
      </label>}
    <button className="am-ghost" type="submit" disabled={state==="busy"||!deviceCode.test(code)}>
      {nextWord(review?"msSetupApproveMachine":"msSetupReviewMachine")}
    </button>
    {review&&<button type="button" className="am-ghost" disabled={state==="busy"} onClick={()=>{setReview(null);setCode("");setState("idle")}}>
      {nextWord("msSetupAnotherCode")}
    </button>}
    {state==="unavailable"&&<p role="status">{nextWord("msSetupEnrollmentFailed")}</p>}
    {state==="approved"&&<p role="status">{nextWord("msSetupEnrollmentDone")}</p>}
  </form>
}
