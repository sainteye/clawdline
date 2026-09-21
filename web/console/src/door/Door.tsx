import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import App, { BRAND_MARK } from "../App.js"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { toast } from "../overlays/toast.js"
import { doorApi, type DoorFailure } from "./api.js"
import { passwordFailureSentence } from "./failure.js"

/**
 * The door: what is on screen when this daemon is answering, and not *this
 * browser*. A pairing code shown on this machine and typed in here — in that
 * direction and never the other — or the password, when one has been set.
 *
 * `legacy/js/door/door.js` is the Swift app's door, byte for byte, and it is
 * the text this file follows line by line — not imported. It cannot be: it
 * imports the whole page (`view/list.js`) and binds its listeners through
 * `core/dom.js`, which looks every id up once at import, before React has drawn
 * any of them. The copy is kept under the guard so a change to the original
 * turns `tools/check-legacy-css.sh` red and this file is read again. The
 * stylesheet is `legacy/door.css`, also the original's, which is why the markup
 * below is `index.html`'s door (lines 1338–1404 there) with the same ids,
 * classes and attributes, and its words are the ones `view/static.js` paints
 * into it.
 *
 * What differs from the original, and why:
 *
 * - **When it shows** is `net/live.js`'s `check`: the open `/v1/health`, never
 *   from the cache, and the door only on an explicit `authed: false`. A health
 *   that does not answer is not about permission, so the console is drawn and
 *   its own light says the daemon is away. Until the page has been let in, the
 *   console is not mounted at all, so no list is read and no event stream is
 *   opened with a credential that will be refused — the original stops its
 *   stream for the same reason. The original asks health again before every reconnect; the
 *   console's stream reconnects on its own here, so the door asks again when
 *   the page comes back into view, when the network does, and every thirty
 *   seconds, and a device revoked under an open page finds the door — drawn
 *   over the console, which stays mounted: the copied modules bind to the
 *   document once, and a console taken down and put back would bind twice.
 * - **After signing in** the original calls `api.start()`, "ask the server
 *   again from the top rather than assuming". Here that is the same health
 *   read, and then the console is mounted for the first time — or, when the
 *   door had come down over a console already running, the page is loaded
 *   again, which is the top.
 * - **A wrong code** is counted on `wrong_code`, which is this daemon's name
 *   for the refusal the Swift app called `forbidden`, and the pairing is over
 *   when the refusal says no tries are left (`tries_left`, which this daemon
 *   counts across pairings, 24 hours at a time) or at five here, whichever
 *   comes first.
 * - The fixture branch (`MOCK`, `mock_code`) is not carried: there is no
 *   fixture server behind this console.
 * - A `#t=` token in the address is not adopted here: `/v1/auth/open` is the
 *   page `clawdline open` lands on, and it trades the token before this one
 *   loads.
 */
export function DoorGate() {
  const [words, setWords] = useState(false)
  const [where, setWhere] = useState<"asking" | "door" | "in">("asking")
  const [password, setPassword] = useState(false)
  const [greet, setGreet] = useState(false)
  // Whether the console has been drawn in this page's life. Once it has, it
  // stays: see the note on revocation above.
  const [drawn, setDrawn] = useState(false)
  const whereRef = useRef(where)
  whereRef.current = where
  const drawnRef = useRef(drawn)
  drawnRef.current = drawn

  const check = useCallback(async () => {
    let h: Awaited<ReturnType<typeof doorApi.health>>
    try {
      h = await doorApi.health()
    } catch {
      // Not even health. Whatever is wrong is not about permission, so the
      // console's own light is what says so.
      if (whereRef.current === "asking") setWhere("in")
      return
    }
    // Only offer the password door when there is a password behind it.
    setPassword(h.password === true)
    setWhere(h.authed === false ? "door" : "in")
  }, [])

  // The catalog, as App reads it (the slot the daemon filled, else
  // /v1/strings): the door is drawn in the reader's words or not at all.
  useEffect(() => {
    const inline = (window as { __strings?: Record<string, string> }).__strings
    void L.loadStrings(async () => inline ?? (await client.strings())).finally(() => setWords(true))
    void check()
  }, [check])

  useEffect(() => {
    const again = () => {
      if (document.visibilityState === "visible") void check()
    }
    document.addEventListener("visibilitychange", again)
    window.addEventListener("online", again)
    const timer = window.setInterval(again, 30_000)
    return () => {
      document.removeEventListener("visibilitychange", again)
      window.removeEventListener("online", again)
      window.clearInterval(timer)
    }
  }, [check])

  // The page is uncovered by App once it has its words; with the door in
  // front and no App, the door does it.
  useEffect(() => {
    if (words && where === "door") document.documentElement.classList.remove("booting")
  }, [words, where])

  useEffect(() => {
    if (where === "in") setDrawn(true)
  }, [where])

  // `toast(T.webDoorPaired)`, once the console that holds `#toast` is drawn.
  useEffect(() => {
    if (!greet || where !== "in") return
    setGreet(false)
    toast(L.strings.webDoorPaired)
  }, [greet, where])

  const signedIn = useCallback(() => {
    if (drawnRef.current) {
      location.reload()
      return
    }
    setGreet(true)
    void check()
  }, [check])

  return (
    <>
      {(where === "in" || drawn) && <App />}
      {words && <Door shown={where === "door"} password={password} onSignedIn={signedIn} />}
    </>
  )
}

interface DoorControl {
  show(): void
  hide(): void
  offerPassword(on: boolean): void
  release(): void
}

/**
 * The door's markup. React draws it once and then leaves it alone: every
 * attribute the original changes from script — `hidden`, `data-step`, the
 * say line's class, the clock, the six boxes — is changed here by `bindDoor`
 * and by nothing else, and none of them is a prop that moves between renders.
 */
function Door({ shown, password, onSignedIn }: { shown: boolean; password: boolean; onSignedIn: () => void }) {
  const host = useRef<HTMLDivElement>(null)
  const mark = useRef<HTMLCanvasElement>(null)
  const control = useRef<DoorControl | null>(null)
  const signedIn = useRef(onSignedIn)
  signedIn.current = onSignedIn

  useLayoutEffect(() => {
    if (!host.current) return
    const bound = bindDoor(host.current, () => signedIn.current())
    control.current = bound
    return () => {
      bound.release()
      control.current = null
    }
  }, [])
  useLayoutEffect(() => {
    // `drawIcon(els["door-mark"], mark, 3)` (`main.js`).
    L.paintIcon(mark.current, BRAND_MARK, 3)
  }, [])
  useLayoutEffect(() => {
    control.current?.offerPassword(password)
  }, [password])
  useLayoutEffect(() => {
    if (shown) control.current?.show()
    else control.current?.hide()
  }, [shown])

  const T = L.strings
  const halves = String(T.webDoorCodeFine).split("{left}")
  const nameAttrs = {
    type: "text",
    maxLength: 40,
    autoComplete: "off",
    autoCapitalize: "words",
    spellCheck: false,
    "data-1p-ignore": "",
    "data-lpignore": "true",
    "data-bwignore": "",
    "data-form-type": "other",
  } as const
  return (
    <div className="door" id="door" data-step="ask" hidden ref={host}>
      <div className="door-card" role="dialog" aria-modal="true" aria-label={T.webDoorLabel}>
        <div className="door-head">
          <canvas id="door-mark" ref={mark} />
          <b>clawdline</b>
        </div>

        <section data-step="ask">
          <p className="lede">{T.webDoorAskLede}</p>
          <p className="fine" dangerouslySetInnerHTML={{ __html: L.wordsHTML(T.webDoorAskFine) }} />
          <label htmlFor="door-name">{T.webDoorName}</label>
          <input id="door-name" name="d1n" {...nameAttrs} />
          <button className="go" id="door-ask" type="button">
            {T.webDoorAsk}
          </button>
          <button className="alt" id="door-to-password" type="button">
            {T.webDoorToPassword}
          </button>
        </section>

        <section data-step="code">
          <p className="lede">{T.webDoorCodeLede}</p>
          <p className="fine">
            {halves[0]}
            <span id="door-left">{T.webDoorTwoMinutes}</span>
            {halves.length > 1 ? halves[1] : ""}
          </p>
          <div className="digits" id="door-digits">
            {[1, 2, 3, 4, 5, 6].map((n) => (
              <input
                key={n}
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={1}
                autoComplete="off"
                name={"c" + n}
                data-1p-ignore=""
                data-lpignore="true"
                data-bwignore=""
                aria-label={L.fillString(T.webDoorDigit, { n })}
              />
            ))}
          </div>
          <button className="go" id="door-confirm" type="button">
            {T.webDoorConfirm}
          </button>
          <button className="alt" id="door-restart" type="button">
            {T.webDoorRestart}
          </button>
        </section>

        <section data-step="password">
          <p className="lede">{T.webDoorPasswordLede}</p>
          <p className="fine">{T.webDoorPasswordFine}</p>
          {/* A real form around the password, so a browser's search for a
              username to go with it ends inside these fields. Nothing submits
              it: the button does the work and Return is stopped from reloading
              the page. */}
          <form id="door-pw-form">
            <label htmlFor="door-password">{T.webDoorPassword}</label>
            <input id="door-password" type="password" autoComplete="current-password" />
            <label htmlFor="door-pw-name">{T.webDoorName}</label>
            <input id="door-pw-name" name="d2n" {...nameAttrs} />
            <button className="go" id="door-pw-go" type="button">
              {T.webDoorPasswordGo}
            </button>
          </form>
          <button className="alt" id="door-to-pair" type="button">
            {T.webDoorToPair}
          </button>
        </section>

        <p className="say" id="door-say" role="status" aria-live="polite" />
      </div>
    </div>
  )
}

/** `phone()` (`core/env.js`): the width the stylesheet switches at, asked each time. */
const phone = () => window.matchMedia("(max-width: 899px)").matches

/**
 * `door.js`, bound to the markup above. The same object, the same methods and
 * the same order; `els` is looked up inside this door rather than once at
 * import, and every listener is removed again when React takes the door away.
 */
function bindDoor(host: HTMLElement, onSignedIn: () => void): DoorControl {
  const T = L.strings
  const byId = <E extends HTMLElement>(id: string) => host.querySelector<E>("#" + id) as E
  const els = {
    door: host,
    name: byId<HTMLInputElement>("door-name"),
    ask: byId<HTMLButtonElement>("door-ask"),
    toPassword: byId<HTMLButtonElement>("door-to-password"),
    digits: byId<HTMLDivElement>("door-digits"),
    confirm: byId<HTMLButtonElement>("door-confirm"),
    restart: byId<HTMLButtonElement>("door-restart"),
    left: byId<HTMLSpanElement>("door-left"),
    password: byId<HTMLInputElement>("door-password"),
    pwName: byId<HTMLInputElement>("door-pw-name"),
    pwGo: byId<HTMLButtonElement>("door-pw-go"),
    toPair: byId<HTMLButtonElement>("door-to-pair"),
    say: byId<HTMLParagraphElement>("door-say"),
    form: byId<HTMLFormElement>("door-pw-form"),
  }
  const boxes = Array.from(els.digits.children) as HTMLInputElement[]
  const off = new AbortController()
  const on = <K extends keyof HTMLElementEventMap>(
    el: HTMLElement,
    type: K,
    fn: (ev: HTMLElementEventMap[K]) => void,
  ) => el.addEventListener(type, fn as EventListener, { signal: off.signal })

  const Door = {
    pairing: null as { id: string; expires: number } | null, // while a code is live on this machine's screen
    wrong: 0, // counted here as well as there
    ticker: undefined as number | undefined,

    show() {
      if (!els.door.hidden) return
      els.door.hidden = false
      if (!els.name.value) els.name.value = suggestName()
      if (!els.pwName.value) els.pwName.value = suggestName()
      this.step("ask")
      // Not on a phone: a keyboard springing up over the explanation of what
      // is about to happen means the explanation is never read.
      if (!phone()) els.name.focus()
    },

    hide() {
      if (els.door.hidden) return
      els.door.hidden = true
      this.stopClock()
      this.say("")
    },

    step(name: string) {
      els.door.dataset.step = name
      this.say("")
      if (name !== "code") this.stopClock()
    },

    /** What went wrong, or what is about to happen: the server's own sentences, shown as they are. */
    say(words: string, calm?: boolean) {
      els.say.textContent = words || ""
      els.say.className = "say" + (calm ? " calm" : "")
    },

    ask() {
      const name = els.name.value.trim() || suggestName()
      els.ask.disabled = true
      this.say(T.webDoorAsking, true)
      doorApi
        .pair(name)
        .then((d) => {
          this.pairing = { id: d.pairing_id, expires: d.expires }
          this.wrong = 0
          this.clearDigits()
          this.step("code")
          this.startClock()
          if (!phone()) boxes[0]?.focus()
        })
        .catch((e: DoorFailure) => {
          // Three requests in ten minutes is the limit because each one puts
          // an alert on somebody's screen, and "rate_limited" on its own reads
          // like a fault rather than like a door working correctly.
          // `e.message` is the server's English. Every refusal but
          // `rate_limited` used to arrive as that message, or as one sentence
          // when there was none — so a `forbidden` and a daemon that is not
          // there read alike. The catalog names each code and tags it.
          this.say(
            L.failureSentence(e, {
              sentence: e.code === "rate_limited" ? T.webDoorRateLimited : "",
              fallback: T.webDoorAskFailed,
            }),
          )
        })
        .then(() => {
          els.ask.disabled = false
        })
    },

    confirm() {
      if (!this.pairing) {
        this.step("ask")
        return
      }
      const code = this.code()
      if (code.length !== 6) {
        this.say(T.webDoorSixDigits)
        return
      }
      els.confirm.disabled = true
      this.say(T.webDoorChecking, true)
      doorApi
        .confirmPair(this.pairing.id, code)
        .then(() => this.signedIn())
        .catch((e: DoorFailure) => {
          // Only a refusal counts. A code that never reached the machine was
          // not a wrong guess.
          if (e.code === "wrong_code") this.wrong += 1
          this.clearDigits()
          if (this.wrong >= 5 || (e.code === "wrong_code" && e.triesLeft === 0)) {
            this.pairing = null
            this.step("ask")
            this.say(T.webDoorFinished)
            return
          }
          // Not every refusal on this press is a wrong code: `rate_limited`,
          // `forbidden` and a door that never answered used to read as "驗證碼
          // 不對", which sends somebody to type the same six digits again.
          this.say(
            L.failureSentence(e, {
              sentence: e.code === "wrong_code" ? T.webDoorWrongCode : "",
              fallback: T.webDoorAskFailed,
            }),
          )
          if (!phone()) boxes[0]?.focus()
        })
        .then(() => {
          els.confirm.disabled = false
        })
    },

    password() {
      const secret = els.password.value
      if (!secret) {
        this.say(T.webDoorNeedPassword)
        return
      }
      // Return in either box asks for this, and so does the button. One
      // attempt at a time whichever of them it came from.
      if (els.pwGo.disabled) return
      els.pwGo.disabled = true
      this.say(T.webDoorChecking, true)
      doorApi
        .password(secret, els.pwName.value.trim() || suggestName())
        .then(() => {
          els.password.value = ""
          this.signedIn()
        })
        .catch((e: DoorFailure) => {
          this.say(passwordFailureSentence(e))
        })
        .then(() => {
          els.pwGo.disabled = false
        })
    },

    /** The cookie is set; ask the server again from the top rather than assuming. */
    signedIn() {
      this.pairing = null
      this.wrong = 0
      this.hide()
      onSignedIn()
    },

    code() {
      return boxes.map((b) => b.value).join("")
    },

    clearDigits() {
      for (const box of boxes) {
        box.value = ""
        box.classList.remove("filled")
      }
    },

    startClock() {
      this.stopClock()
      const tick = () => {
        if (!this.pairing) return
        const left = Math.max(0, this.pairing.expires - Math.floor(Date.now() / 1000))
        if (left <= 0) {
          this.pairing = null
          this.step("ask")
          this.say(T.webDoorExpired)
          return
        }
        const m = Math.floor(left / 60)
        const sec = left % 60
        els.left.textContent = m + ":" + (sec < 10 ? "0" : "") + sec
      }
      tick()
      this.ticker = window.setInterval(tick, 1000)
    },

    stopClock() {
      window.clearInterval(this.ticker)
      this.ticker = undefined
    },
  }

  /* ---- the six boxes ------------------------------------------------------
     A code read off another screen and typed with a thumb. Paste the whole code
     anywhere in it, type straight through without tabbing, backspace out of an
     empty box.
     ------------------------------------------------------------------------ */
  const fill = (from: number, text: string) => {
    const digits = text.replace(/\D/g, "").split("")
    for (let i = from; i < boxes.length && digits.length; i++) {
      boxes[i].value = digits.shift() as string
      boxes[i].classList.add("filled")
    }
    const next = Math.min(boxes.length - 1, from + text.replace(/\D/g, "").length)
    boxes[next].focus()
    // Six digits in and there is nothing else this screen is for.
    if (Door.code().length === 6) Door.confirm()
  }
  boxes.forEach((box, index) => {
    on(box, "input", () => {
      const typed = box.value
      box.value = ""
      fill(index, typed)
    })
    on(box, "keydown", (ev) => {
      if (ev.key === "Backspace" && !box.value && index > 0) {
        ev.preventDefault()
        boxes[index - 1].value = ""
        boxes[index - 1].classList.remove("filled")
        boxes[index - 1].focus()
      } else if (ev.key === "ArrowLeft" && index > 0) {
        ev.preventDefault()
        boxes[index - 1].focus()
      } else if (ev.key === "ArrowRight" && index < boxes.length - 1) {
        ev.preventDefault()
        boxes[index + 1].focus()
      } else if (ev.key === "Enter") {
        ev.preventDefault()
        Door.confirm()
      }
    })
    on(box, "paste", (ev) => {
      const text = ev.clipboardData?.getData("text") || ""
      if (!/\d/.test(text)) return
      ev.preventDefault()
      fill(index, text)
    })
    on(box, "focus", () => box.select())
  })

  on(els.ask, "click", () => Door.ask())
  on(els.confirm, "click", () => Door.confirm())
  on(els.restart, "click", () => {
    Door.pairing = null
    Door.step("ask")
  })
  on(els.toPassword, "click", () => Door.step("password"))
  on(els.toPair, "click", () => Door.step("ask"))
  on(els.pwGo, "click", () => Door.password())
  on(els.name, "keydown", (ev) => {
    if (ev.key === "Enter") Door.ask()
  })
  // The password box is in a form, so Return would otherwise reload the page
  // and lose whatever was typed. It means the same thing it always did.
  on(els.form, "submit", (ev) => {
    ev.preventDefault()
    Door.password()
  })
  on(els.password, "keydown", (ev) => {
    if (ev.key === "Enter") Door.password()
  })
  on(els.pwName, "keydown", (ev) => {
    if (ev.key === "Enter") Door.password()
  })

  return {
    show: () => Door.show(),
    hide: () => Door.hide(),
    offerPassword: (yes) => {
      // A link to a door that was never built teaches somebody to distrust
      // the rest of the page.
      els.toPassword.hidden = !yes
    },
    release: () => {
      Door.stopClock()
      off.abort()
    },
  }
}

/**
 * What to call this device, guessed from the browser so that nobody has to
 * think of a name for a thing they are holding. Editable, because the guess is
 * often "Chrome on Mac" when what matters is "the work laptop".
 *
 * **Not translated, deliberately.** It is stored on the machine and read back
 * in its list of paired devices, in whatever language that machine is set to.
 */
function suggestName(): string {
  const ua = navigator.userAgent || ""
  if (/iPhone/.test(ua)) return "iPhone"
  if (/iPad/.test(ua)) return "iPad"
  if (/Android/.test(ua)) return /Mobile/.test(ua) ? "Android phone" : "Android tablet"
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /OPR\//.test(ua)
      ? "Opera"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Firefox\//.test(ua)
          ? "Firefox"
          : /Safari\//.test(ua)
            ? "Safari"
            : ""
  const machine = /Macintosh|Mac OS X/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : ""
  if (browser && machine) return browser + " on " + machine
  return browser || machine || "A browser"
}
