import { useEffect, useRef, useState } from "react"
import { nextWord } from "../../next-strings.js"
import type { CloudMachine } from "../../cloud/copied.js"

const offers = {
  instabox: { name: "InstaBox", dashboard: "https://dashboard.instabox.sh/", pricing: "https://instabox.sh/pricing", detail: "msSetupInstaBox", template: "msSetupTemplateInstaBox" },
  digitalocean: { name: "DigitalOcean", dashboard: "https://cloud.digitalocean.com/", pricing: "https://www.digitalocean.com/pricing/droplets", detail: "msSetupDigitalOcean", template: "msSetupTemplateDigitalOcean" },
} as const
type Provider = keyof typeof offers

// The provider account owns checkout. Opening either provider never submits an order.
const install = `bash -o pipefail -c 'curl -fsSL https://clawdline.com/install.sh | sh -s -- --headless --no-app | sed "/^[[:space:]]*http:\\/\\/127\\.0\\.0\\.1:/d"'`

export function CloudMachineSetup({ apiOrigin, machines, refresh, pair, close }: {
  apiOrigin: string
  machines: readonly CloudMachine[]
  refresh: () => Promise<boolean>
  pair: (id: string) => void
  close: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [provider, setProvider] = useState<Provider | null>(null)
  const [machineID, setMachineID] = useState("")
  const [checkState, setCheckState] = useState<"idle" | "checking" | "failed">("idle")
  const [copied, setCopied] = useState<"ok" | "failed" | null>(null)
  const linux = machines.filter(machine => machine.kind === "linux" || machine.kind === "linux_aws")
  const machine = linux.find(candidate => candidate.id === machineID)
  const ready = machine?.freshness === "current" && machine.pairing === "paired"
  const offer = provider && offers[provider]
  useEffect(() => { dialog.current?.showModal() }, [])

  const check = async () => {
    setCheckState("checking")
    setCheckState(await refresh() ? "idle" : "failed")
  }
  const copyInstall = async () => {
    try { await navigator.clipboard.writeText(install); setCopied("ok") }
    catch { setCopied("failed") }
  }

  return <dialog ref={dialog} className="am-cloud-setup" aria-labelledby="am-cloud-setup-title" onClose={close}>
    <header className="am-cloud-setup-head">
      <h2 id="am-cloud-setup-title">{nextWord("msSetupCloud")}</h2>
      <button className="am-ghost" type="button" onClick={() => dialog.current?.close()}>{nextWord("msSetupClose")}</button>
    </header>
    <div className="am-cloud-setup-body">
      {!offer ? <fieldset className="am-cloud-providers">
        <legend>{nextWord("msSetupChoose")}</legend>
        {(Object.entries(offers) as [Provider, typeof offers[Provider]][]).map(([id, option]) =>
          <button className="am-cloud-provider" type="button" key={id} onClick={() => setProvider(id)}>
            <strong>{option.name}</strong><span>{nextWord(option.detail)}</span>
          </button>)}
      </fieldset> : <>
        <div className="am-cloud-setup-actions">
          <button className="am-ghost" type="button" onClick={() => { setProvider(null); setCopied(null); setMachineID(""); setCheckState("idle") }}>{nextWord("msSetupChange")}</button>
          <a href={offer.pricing} target="_blank" rel="noopener noreferrer">{nextWord("msSetupPricing")}</a>
        </div>
        <ol className="am-cloud-setup-steps">
          <li>
            <h3>{nextWord("msSetupProviderStep", { provider: offer.name })}</h3>
            <p>{nextWord("msSetupExisting")}</p>
            <p>{nextWord(offer.template)}</p>
            <a href={offer.dashboard} target="_blank" rel="noopener noreferrer">{nextWord("msSetupDashboard", { provider: offer.name })}</a>
          </li>
          <li>
            <h3>{nextWord("msSetupInstall")}</h3>
            <p>{nextWord("msSetupInstallHelp")}</p>
            <pre><code>{install}</code></pre>
            <button className="am-ghost" type="button" onClick={() => void copyInstall()}>{nextWord("msSetupCopy")}</button>
            {copied && <span role="status">{nextWord(copied === "ok" ? "msSetupCopied" : "msSetupCopyFailed")}</span>}
            <p>{nextWord("msSetupLoginHelp")}</p>
            <pre><code>clawdline --lang en cloud login | sed '/^approve at /d'</code></pre>
            <CloudMachineEnrollment apiOrigin={apiOrigin} onRegistered={id=>{setMachineID(id);void check()}} />
            <pre><code>clawdline cloud on</code></pre>
            <p>{nextWord("msSetupHealthHelp")}</p>
            <pre><code>{'systemctl --user is-active clawdline-next.service\nclawdline update --json'}</code></pre>
          </li>
          <li>
            <h3>{nextWord("msSetupPair")}</h3>
            <label>
              {nextWord("msSetupMachineLabel")}
              <select value={machineID} onChange={event => setMachineID(event.currentTarget.value)}>
                <option value="">{nextWord("msSetupMachinePlaceholder")}</option>
                {linux.map(candidate => <option key={candidate.id} value={candidate.id}>{candidate.name || candidate.label}</option>)}
              </select>
            </label>
            <div className="am-cloud-setup-actions">
              <button type="button" className="am-ghost" onClick={() => void check()} disabled={checkState === "checking"}>{nextWord("msSetupRefresh")}</button>
              {machine?.pairing === "not_paired" && <button type="button" className="am-primary" onClick={() => pair(machine.id)}>{nextWord("msPair")}</button>}
            </div>
            <p role="status">{nextWord(checkState === "checking" ? "msSetupChecking" : checkState === "failed" ? "msSetupRefreshFailed" : ready ? "msSetupConnected" : "msSetupWaiting")}</p>
          </li>
        </ol>
      </>}
    </div>
  </dialog>
}


type MachineReview = { name: string; fingerprint: string; review_digest: string }
const deviceCode = /^[A-Z0-9]{4}-?[A-Z0-9]{4}$/

function CloudMachineEnrollment({apiOrigin,onRegistered}:{apiOrigin:string;onRegistered:(id:string)=>void}) {
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
        setCode("");setReview(null);setState("approved");onRegistered(result.machine_id)
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
