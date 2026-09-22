// Pairing from the machine list, against a fake `pairViewer`:
// `node --test web/console/src/cloud/pair.test.ts`.
//
// No control plane and no machine: the start function below stands where
// `pairViewer` stands and behaves as its loop does — it hands the offer to
// `onOffer`, then claims, sleeping between claims with the `sleep` it was given.
// What is checked is what a person is shown at each point, and that stopping
// and expiring are told apart from a pairing that did not belong to this
// browser.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts. It sits on one line because the directive answers for the line the path is on.
import { INVITATION_KEY, PairingRun, dropInvitation, invitationInHash, pairingCommand, pairingEnding, takeInvitation, watchInvitations, type OpenedPairing, type PairState, type PairStart, type PendingOffer } from "./pair.ts"

const OFFER: PendingOffer = {
  pairingID: "pair_1",
  expiresAt: Date.UTC(2026, 8, 21, 4, 0, 0),
  fingerprint: "ABCD-EFGH-IJKL-MNOP",
  fragment: "eyJhY2NvdW50X2lkIjoiYWNjdF8xIn0",
}

const OPENED: OpenedPairing = { machineID: "mac_linux", machineFingerprint: "QRST-UVWX-YZ23-4567" }

/** `pairViewer`'s loop: offer, then claim until `answers` says otherwise. */
function viewer(answers: Array<"pending" | OpenedPairing | { code: string }>): PairStart {
  return async ({ onOffer, sleep }) => {
    onOffer(OFFER)
    for (const answer of answers) {
      if (answer === "pending") {
        await sleep(2000)
        continue
      }
      if ("code" in answer) throw Object.assign(new Error(answer.code), { code: answer.code })
      return answer
    }
    throw Object.assign(new Error("offer_expired"), { code: "offer_expired" })
  }
}

/** Timers that fire when told to, so a test decides when two seconds have passed. */
function manualTimers() {
  const due: Array<{ fn: () => void; cleared: boolean }> = []
  return {
    timers: {
      setTimeout: ((fn: () => void) => {
        const entry = { fn, cleared: false }
        due.push(entry)
        return entry
      }) as unknown as typeof setTimeout,
      clearTimeout: ((entry: { cleared: boolean }) => {
        entry.cleared = true
      }) as unknown as typeof clearTimeout,
    },
    fire() {
      const next = due.shift()
      if (next && !next.cleared) next.fn()
    },
    pending: () => due.filter((d) => !d.cleared).length,
  }
}

const tick = () => new Promise((resolve) => setImmediate(resolve))

test("a run shows the code and this browser's fingerprint, then the machine's once it answers", async () => {
  const seen: PairState[] = []
  const clock = manualTimers()
  const run = new PairingRun(viewer(["pending", OPENED]), (s: PairState) => seen.push(s), clock.timers)
  const ended = run.begin()
  await tick()
  assert.deepEqual(run.state, { phase: "waiting", fragment: OFFER.fragment, fingerprint: OFFER.fingerprint, expiresAt: OFFER.expiresAt })
  clock.fire()
  const last = await ended
  assert.deepEqual(last, { phase: "paired", machineID: "mac_linux", machineFingerprint: "QRST-UVWX-YZ23-4567", fingerprint: OFFER.fingerprint })
  assert.deepEqual(
    seen.map((s) => s.phase),
    ["asking", "waiting", "paired"],
  )
})

test("begin is once: a second press is the same run, not a second offer", async () => {
  let started = 0
  const start: PairStart = async (hooks) => {
    started += 1
    return viewer([OPENED])(hooks)
  }
  const run = new PairingRun(start, () => {})
  const first = run.begin()
  const second = run.begin()
  await first
  await second
  assert.equal(started, 1)
})

test("stopping says until when the code still works, because nothing withdraws it", async () => {
  const clock = manualTimers()
  const run = new PairingRun(viewer(["pending", "pending", OPENED]), () => {}, clock.timers)
  const ended = run.begin()
  await tick()
  assert.equal(run.state.phase, "waiting")
  run.stop()
  assert.deepEqual(run.state, { phase: "stopped", expiresAt: OFFER.expiresAt })
  assert.equal(clock.pending(), 0, "the sleep in progress was cancelled, not left to fire")
  assert.deepEqual(await ended, { phase: "stopped", expiresAt: OFFER.expiresAt })
})

test("a handover that lands as the person stops is reported as paired, because the keys are stored", async () => {
  let release: (value: OpenedPairing) => void = () => {}
  const start: PairStart = ({ onOffer }) => {
    onOffer(OFFER)
    return new Promise((resolve) => {
      release = resolve
    })
  }
  const run = new PairingRun(start, () => {})
  const ended = run.begin()
  await tick()
  run.stop()
  assert.equal(run.state.phase, "stopped")
  release(OPENED)
  assert.equal((await ended).phase, "paired")
})

test("stopping before the code arrives does not then show the code", async () => {
  let deliver: () => void = () => {}
  const start: PairStart = ({ onOffer, sleep }) =>
    new Promise<OpenedPairing>((_resolve, reject) => {
      deliver = () => {
        onOffer(OFFER)
        sleep(2000).then(() => {}, reject)
      }
    })
  const run = new PairingRun(start, () => {})
  const ended = run.begin()
  assert.equal(run.state.phase, "asking")
  run.stop()
  assert.deepEqual(run.state, { phase: "stopped", expiresAt: null })
  deliver()
  assert.deepEqual(await ended, { phase: "stopped", expiresAt: OFFER.expiresAt })
})

test("the three endings are three words: ran out, not this browser's, and anything else with its code", async () => {
  for (const code of ["offer_expired", "pairing_expired", "invitation_expired"]) {
    assert.deepEqual(pairingEnding({ code }), { phase: "expired" }, code)
  }
  for (const code of ["wrong_invitation", "wrong_claimant", "pairing_gone", "wrong_account", "wrong_sender", "wrong_pairing"]) {
    assert.deepEqual(pairingEnding({ code }), { phase: "refused", code }, code)
  }
  assert.deepEqual(pairingEnding({ code: "bad_handover" }), { phase: "failed", code: "bad_handover" })
  // A browser's own failure says what it was, not the fallback.
  assert.deepEqual(pairingEnding(new TypeError("Failed to fetch")), { phase: "failed", code: "TypeError" })
  assert.deepEqual(pairingEnding({ name: "NotSupportedError", code: 9 }), { phase: "failed", code: "NotSupportedError" })
  assert.deepEqual(pairingEnding(new Error("nothing named")), { phase: "failed", code: "pairing_failed" })
  const run = new PairingRun(viewer([{ code: "wrong_claimant" }]), () => {})
  assert.deepEqual(await run.begin(), { phase: "refused", code: "wrong_claimant" })
})

test("the line the machine is given needs no quoting", () => {
  assert.equal(pairingCommand(OFFER.fragment), "clawdline cloud pair -offer " + OFFER.fragment)
  // base64url: the alphabet a shell passes through untouched.
  assert.match(OFFER.fragment, /^[A-Za-z0-9_-]+$/)
})

/** A tab: an address, a history and session storage. */
function tab(hash: string) {
  const kept = new Map<string, string>()
  const replaced: string[] = []
  return {
    location: { hash, pathname: "/", search: "" },
    history: {
      state: { page: "sessions" },
      replaceState(_state: unknown, _unused: string, url: string) {
        replaced.push(url)
      },
    },
    sessionStorage: {
      getItem: (key: string) => kept.get(key) ?? null,
      setItem: (key: string, value: string) => void kept.set(key, value),
      removeItem: (key: string) => void kept.delete(key),
    },
    kept,
    replaced,
  }
}

test("a machine's link is taken out of the address and kept for the sign-in round trip", () => {
  const opened = tab("#pair=eyJ2IjoxfQ")
  assert.equal(takeInvitation(opened), "eyJ2IjoxfQ")
  assert.deepEqual(opened.replaced, ["/"], "the secret leaves the address bar and the history entry")
  assert.equal(opened.kept.get(INVITATION_KEY), "eyJ2IjoxfQ")

  // Back from signing in: no fragment now, and the link is still there.
  opened.location.hash = ""
  assert.equal(takeInvitation(opened), "eyJ2IjoxfQ")
  dropInvitation(opened.sessionStorage)
  assert.equal(takeInvitation(opened), null)
})

test("only #pair= with something after it is a machine's link", () => {
  assert.equal(invitationInHash("#pair="), null)
  assert.equal(invitationInHash("#page=devices"), null)
  assert.equal(invitationInHash(""), null)
  assert.equal(invitationInHash("#pair=abc"), "abc")
})

test("an already-open Home Screen app reads a pairing link when iOS brings it forward", () => {
  const opened = tab("")
  const windowListeners = new Map<string, () => void>()
  const documentListeners = new Map<string, () => void>()
  const scope = {
    ...opened,
    addEventListener: (name: string, listener: () => void) => void windowListeners.set(name, listener),
    removeEventListener: (name: string) => void windowListeners.delete(name),
    document: {
      addEventListener: (name: string, listener: () => void) => void documentListeners.set(name, listener),
      removeEventListener: (name: string) => void documentListeners.delete(name),
    },
  }
  const invitations: string[] = []
  const stop = watchInvitations(scope, (raw) => invitations.push(raw))

  scope.location.hash = "#pair=first"
  windowListeners.get("focus")?.()
  assert.deepEqual(invitations, ["first"])
  assert.deepEqual(opened.replaced, ["/"])

  // WebKit may send more than one resume signal. The one-use invitation is
  // shown once rather than restarting the pairing run for every signal.
  windowListeners.get("pageshow")?.()
  documentListeners.get("visibilitychange")?.()
  assert.deepEqual(invitations, ["first"])

  scope.location.hash = "#pair=second"
  windowListeners.get("hashchange")?.()
  assert.deepEqual(invitations, ["first", "second"])

  stop()
  assert.equal(windowListeners.size, 0)
  assert.equal(documentListeners.size, 0)
})

test("the wait between claims calls the timer as a browser requires: with no receiver", async () => {
  // Measured in Chromium before this test existed: `this.timers.setTimeout(…)`
  // threw "Illegal invocation" at the first 202, and every pairing ended
  // `failed` half a second after it began. Node's timers do not care, so this
  // one does, the way a window's do.
  const strict = {
    setTimeout: function (this: unknown, fn: () => void, ms: number) {
      if (this !== undefined && this !== globalThis) throw new TypeError("Illegal invocation")
      return setTimeout(fn, Math.min(ms, 5))
    } as unknown as typeof setTimeout,
    clearTimeout: function (this: unknown, timer: ReturnType<typeof setTimeout>) {
      if (this !== undefined && this !== globalThis) throw new TypeError("Illegal invocation")
      clearTimeout(timer)
    } as unknown as typeof clearTimeout,
  }
  const run = new PairingRun(viewer(["pending", "pending", OPENED]), () => {}, strict)
  assert.equal((await run.begin()).phase, "paired")
  const stopped = new PairingRun(viewer(["pending", "pending", OPENED]), () => {}, strict)
  const ended = stopped.begin()
  await tick()
  stopped.stop()
  assert.equal((await ended).phase, "stopped")
})
