// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { CallAudioError, type CallAudioSource, type CallFailure } from "./bot-call.ts"

interface RecognitionResult {
  isFinal: boolean
  0: { transcript: string }
}

interface LocalRecognition {
  processLocally: boolean
  lang: string
  onresult: ((event: { resultIndex: number; results: ArrayLike<RecognitionResult> }) => void) | null
  onend: (() => void) | null
  onerror: ((event: { error: string }) => void) | null
  start(track: MediaStreamTrack): void
  abort(): void
}

interface RecognitionConstructor {
  new(): LocalRecognition
  available(options: { langs: string[]; processLocally: boolean }): Promise<string>
}

type SpeechWindow = Window & { SpeechRecognition?: RecognitionConstructor; webkitSpeechRecognition?: RecognitionConstructor }
function localRecognizer() {
  const browser = window as SpeechWindow
  const Recognition = browser.SpeechRecognition ?? browser.webkitSpeechRecognition
  return Recognition && "processLocally" in Recognition.prototype && typeof Recognition.available === "function"
    ? Recognition : undefined
}

export function botCallAudioAvailable() {
  return typeof window !== "undefined" && !!(localRecognizer() && typeof navigator.mediaDevices?.getUserMedia === "function"
    && typeof window.speechSynthesis !== "undefined" && typeof window.SpeechSynthesisUtterance === "function")
}

function stop(stream: MediaStream) {
  stream.getTracks().forEach(track => track.stop())
}

function abortError() {
  return new DOMException("Aborted", "AbortError")
}

/** The selected language pack and playback voice must be installed on this device. */
export function browserCallAudio(language: string): CallAudioSource {
  const locale = new Intl.Locale(language || "en").maximize()
  const lang = [locale.language, locale.region].filter(Boolean).join("-")
  return {
    async open(signal, heard, failed) {
      const Recognition = localRecognizer()
      if (!Recognition || !botCallAudioAvailable()) throw new CallAudioError("unsupported")
      const available = await Recognition.available({ langs: [lang], processLocally: true }).catch(() => "unavailable")
      if (available !== "available") throw new CallAudioError("language")
      if (signal.aborted) throw abortError()
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true } })
      const track = stream.getAudioTracks()[0]
      if (!track || track.readyState !== "live" || signal.aborted) {
        stop(stream)
        if (signal.aborted) throw abortError()
        throw new CallAudioError("microphone")
      }
      track.enabled = false
      let listening = false
      let closed = false
      let recognizer: LocalRecognition | undefined
      const cancelRecognition = () => {
        const pending = recognizer
        recognizer = undefined
        if (!pending) return
        pending.onresult = pending.onend = pending.onerror = null
        pending.abort()
      }
      const report = (code: CallFailure) => {
        if (!closed && !signal.aborted) failed(code)
      }
      const trackEnded = () => report("disconnected")
      track.addEventListener("ended", trackEnded)
      const begin = () => {
        try {
          const run = new Recognition()
          recognizer = run
          run.lang = lang
          run.processLocally = true
          run.onresult = event => {
            if (recognizer !== run || !listening || closed || signal.aborted) return
            const text = Array.from(event.results).slice(event.resultIndex)
              .filter(result => result.isFinal).map(result => result[0].transcript).join(" ")
            if (text.trim()) heard(text)
          }
          run.onerror = event => {
            if (recognizer !== run || event.error === "no-speech") return
            report(event.error === "not-allowed" || event.error === "audio-capture" ? "microphone"
              : event.error === "language-not-supported" ? "language" : "audio")
          }
          run.onend = () => {
            if (recognizer !== run) return
            recognizer = undefined
            if (listening && !closed && !signal.aborted) begin()
          }
          run.start(track)
        } catch {
          report("audio")
        }
      }
      return {
        listen(enabled) {
          if (closed || signal.aborted || listening === enabled) return
          listening = enabled
          track.enabled = enabled
          if (enabled) begin()
          else cancelRecognition()
        },
        play(text, playback) {
          if (closed || signal.aborted || playback.aborted) return Promise.reject(abortError())
          const synth = window.speechSynthesis
          const voices = synth.getVoices().filter(voice => voice.localService)
          const voice = voices.find(v => v.lang.toLowerCase() === lang.toLowerCase())
            ?? voices.find(v => v.lang.toLowerCase().split("-")[0] === lang.toLowerCase().split("-")[0])
          if (!voice) return Promise.reject(new CallAudioError("audio"))
          return new Promise<void>((resolve, reject) => {
            const line = new SpeechSynthesisUtterance(text)
            line.voice = voice
            line.lang = voice.lang
            const done = (error?: Error) => {
              line.onend = line.onerror = null
              playback.removeEventListener("abort", cancel)
              if (error) reject(error)
              else resolve()
            }
            const cancel = () => {
              done(abortError())
              synth.cancel()
            }
            line.onend = () => done()
            line.onerror = () => done(new CallAudioError("audio"))
            playback.addEventListener("abort", cancel, { once: true })
            try { synth.speak(line) }
            catch { done(new CallAudioError("audio")) }
          })
        },
        close() {
          if (closed) return
          closed = true
          track.removeEventListener("ended", trackEnded)
          stop(stream)
          cancelRecognition()
        },
      }
    },
  }
}
