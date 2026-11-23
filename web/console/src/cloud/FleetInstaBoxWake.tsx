import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { instaBoxWakeReason, readInstaBoxBinding, wakeInstaBox } from "./instabox-wake.js"

type WakeState = { kind: "idle" | "waking" | "accepted" } | { kind: "failed"; message: string }

export function FleetInstaBoxWake({ machine, origin, onWakeAvailability }: {
  machine: string
  origin: string
  onWakeAvailability?: (machine: string, ready: boolean) => void
}) {
  const [bindingReady, setBindingReady] = useState(false)
  const [wake, setWake] = useState<WakeState>({ kind: "idle" })
  const pending = useRef<AbortController | null>(null)

  useEffect(() => {
    const read = new AbortController()
    void readInstaBoxBinding(origin, machine, read.signal).then((binding) => {
      if (!read.signal.aborted) setBindingReady(binding.configured === true && binding.credential_ready === true)
    }, () => {})
    return () => {
      read.abort()
      pending.current?.abort()
      pending.current = null
    }
  }, [machine, origin])

  useEffect(() => {
    if (!bindingReady || !onWakeAvailability) return
    onWakeAvailability(machine, true)
    return () => onWakeAvailability(machine, false)
  }, [bindingReady, machine, onWakeAvailability])

  async function requestWake() {
    if (!bindingReady || pending.current) return
    const request = new AbortController()
    pending.current = request
    setWake({ kind: "waking" })
    try {
      await wakeInstaBox(origin, machine, request.signal)
      if (!request.signal.aborted) setWake({ kind: "accepted" })
    } catch (error) {
      if (!request.signal.aborted) setWake({ kind: "failed", message: instaBoxWakeReason(error) })
    } finally {
      if (pending.current === request) pending.current = null
    }
  }

  if (!bindingReady) return null
  const waking = wake.kind === "waking"
  return <div className="cloud-all-wake">
    <button type="button" className="cloud-detail-act" disabled={waking} aria-busy={waking}
      onClick={() => void requestWake()}>{nextWord(waking ? "msInstaWaking" : "msInstaWake")}</button>
    {wake.kind === "accepted" && <p role="status">{nextWord("msInstaAccepted")}</p>}
    {wake.kind === "failed" && <p role="alert">{wake.message}</p>}
  </div>
}
