import "../../src/loading-skeleton.css"
import "../../src/legacy/tokens.css"
import "../../src/legacy/base.css"
import "../../src/legacy/header.css"
import "../../src/legacy/shell.css"
import "../../src/legacy/list.css"
import "../../src/legacy/schedules.css"
import "../../src/pages/schedules/run-loading.css"
import "../../src/legacy/detail.css"
import "../../src/legacy/transcript.css"
import "../../src/legacy/composer.css"
import "../../src/legacy/live.css"
import "../../src/legacy/agents.css"
import "../../src/legacy/status-line.css"
import "../../src/legacy/sheets.css"
import "../../src/legacy/coordinator.css"
import "../../src/legacy/door.css"
import "../../src/legacy/responsive.css"
import "../../src/legacy/pages.css"
import "../../src/legacy/devices.css"
import "../../src/legacy/projects.css"
import "../../src/legacy/worktrees.css"
import "../../src/legacy/plan.css"
import "../../src/legacy/board.css"
import "../../src/legacy/session-board.css"
import "../../src/legacy/documents.css"
import "../../src/legacy/user-messages.css"
import "../../src/legacy/snippets.css"
import "../../src/side-border-overrides.css"
import "../../src/shell.css"
import "../../src/sidebar-scroll.css"
import "../../src/ui/disclosure-motion.css"
import "../../src/brand-handle.css"
import "../../src/font-scale.css"
// Synthetic failure injection around real Composer, transcript and voice code.
// No physical microphone, daemon, provider or remote transcription is used.
import { createRoot } from "react-dom/client"
import { Composer } from "../../src/session/Composer.js"
import { TranscriptEntries } from "../../src/session/Transcript.js"
import { StatusLine } from "../../src/session/StatusLine.js"
import { activateCatalog } from "../../src/catalog.js"
import { loadStrings, publish } from "../../src/legacy/bridge.js"
import * as Voice from "../../src/legacy/voice-bridge.js"

const q = new URLSearchParams(location.search)
const lang = q.get("lang") === "en" ? "en" : "zh-Hant"
const mode = q.get("mode") || "normal"
let finish: (() => void) | null = null
let resolveRead: ((value: { text: string }) => void) | null = null
let failRead = mode === "failed"
let posts = 0, opens = 0
const tracks: { readyState: string; stop(): void }[] = []
const status = () => {
  const label = document.getElementById("test-microphone")
  if (label) label.textContent = (lang === "en" ? "Test microphone: " : "測試麥克風：") +
    (tracks.some(track => track.readyState === "live") ? (lang === "en" ? "listening" : "收音中") : (lang === "en" ? "off" : "已關閉"))
}
// A complete one-second WAV exercises the browser's real decoder/resampler.
function wave(): Blob {
  const bytes = new ArrayBuffer(44 + 32000), d = new DataView(bytes)
  const ascii = (offset: number, value: string) => [...value].forEach((c, i) => d.setUint8(offset + i, c.charCodeAt(0)))
  ascii(0, "RIFF"); d.setUint32(4, bytes.byteLength - 8, true); ascii(8, "WAVEfmt ")
  d.setUint32(16, 16, true); d.setUint16(20, 1, true); d.setUint16(22, 1, true)
  d.setUint32(24, 16000, true); d.setUint32(28, 32000, true); d.setUint16(32, 2, true); d.setUint16(34, 16, true)
  ascii(36, "data"); d.setUint32(40, 32000, true)
  for (let i = 0; i < 16000; i++) d.setInt16(44 + i * 2, Math.sin(i * Math.PI * 440 / 8000) * 5000, true)
  return new Blob([bytes], { type: "audio/wav" })
}
class FixtureRecorder {
  state = "inactive"; mimeType = "audio/wav"
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null
  onerror: (() => void) | null = null
  start() {
    this.state = "recording"
    if (mode === "partial") this.ondataavailable?.({ data: wave() })
    finish = () => { if (!["partial", "empty"].includes(mode)) this.ondataavailable?.({ data: wave() }); this.onstop?.() }
  }
  requestData() {}
  stop() {
    this.state = "inactive"
    if (!["delayed", "missing", "partial"].includes(mode)) queueMicrotask(() => finish?.())
  }
}
if (mode !== "native") Object.defineProperty(window, "MediaRecorder", { configurable: true, value: FixtureRecorder })
Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia: async () => {
  opens++
  if (mode === "native") {
    const context = new AudioContext()
    const destination = context.createMediaStreamDestination()
    const oscillator = context.createOscillator()
    oscillator.connect(destination); oscillator.start(); void context.resume()
    const track = destination.stream.getAudioTracks()[0]
    const stop = track.stop.bind(track)
    track.stop = () => { stop(); oscillator.stop(); void context.close(); status() }
    tracks.push(track)
    return destination.stream
  }
  const track = { readyState: "live", stop() { this.readyState = "ended"; status() } }
  tracks.push(track); queueMicrotask(status)
  return { getTracks: () => [track] }
} } })
if (mode === "preparing") {
  const NativeAudio = window.AudioContext
  class StalledAudio extends NativeAudio {
    decodeAudioData(): Promise<AudioBuffer> { return new Promise(() => {}) }
  }
  Object.defineProperty(window, "AudioContext", { configurable: true, value: StalledAudio })
}
const real = window.fetch.bind(window)
const json = (value: unknown, code = 200) => new Response(JSON.stringify(value), { status: code, headers: { "Content-Type": "application/json" } })
window.fetch = async (input, init) => {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, location.href)
  if (!url.pathname.startsWith("/v1/")) return real(input, init)
  if (url.pathname === "/v1/voice/source") return json({ machine: "fixture", platform: "darwin", name: "Test host" })
  if (url.pathname === "/v1/personas") return json({ personas: [] })
  return json({ error: { code: "not_found", message: "Synthetic fixture" } }, 404)
}
const words = await real("/catalogs/" + lang + ".json").then(response => response.json())
activateCatalog(words, lang)
await loadStrings(async () => words)
const row = { id: "%voice-fixture", sessionId: "00000000-0000-4000-8000-00000000a11e", assistant: "codex", backend: "tmux", state: "idle",
  title: lang === "en" ? "Voice input recovery" : "語音輸入恢復", cwd: "/synthetic/voice", project: "Voice test", agents: [], shells: [], activity: { at: Date.now() / 1000 } }
publish([row] as never, row.id, "", row.id, [])
const text = lang === "en" ? "The recovered recording appears here without recording again." : "這段錄音已恢復，不必重新再講一次。"
const remote = { key: row.id, machineID: "fixture", assistant: "codex", canSend: true, allowPictures: false,
  transcribe: async () => {
    posts++
    if (failRead) throw Object.assign(new Error("Synthetic offline"), { code: "offline" })
    if (mode === "reading") return new Promise<{ text: string }>(resolve => { resolveRead = resolve })
    return { text }
  }, onSend: async () => { throw new Error("Sending is disabled in this fixture") } }
const entries = [
  { id: "fixture-user", role: "user", text: lang === "en" ? "Check that Stop ends the microphone and keeps my recording if processing fails." : "確認按停止就停止收音，處理失敗時也能保住剛剛說的話。", at: Date.now() / 1000 - 60 },
  { id: "fixture-assistant", role: "assistant", text: lang === "en" ? "Tap the microphone, then Stop. Failure recovery keeps this recording for retry." : "點麥克風開始，再按停止。若後續處理失敗，可保留這段錄音並重試轉文字。", at: Date.now() / 1000 - 45 },
]
document.documentElement.classList.remove("booting")
createRoot(document.getElementById("root")!).render(<main className="app" data-view="detail" style={{ width: "100%", height: "100dvh", display: "flex" }}>
  <section className="pane-detail" style={{ display: "flex", flexDirection: "column", flex: 1, minWidth: 0 }}>
    <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--line)", color: "var(--accent)", fontSize: 13 }}>
      <strong id="test-case">{lang === "en" ? "Local fault injection · real UI" : "本機故障注入・實際介面元件"}</strong>
      <div id="test-microphone" style={{ color: "var(--dim)", marginTop: 4 }}>{lang === "en" ? "Test microphone: off" : "測試麥克風：已關閉"}</div>
    </div>
    <div className="transcript" style={{ flex: 1, overflow: "auto" }}><TranscriptEntries entries={entries as never} assistant="codex" /></div>
    <Composer row={null} remote={remote} onDid={() => {}} />
    <StatusLine row={row as never} />
  </section>
</main>)
Object.assign(window, { voiceFixture: {
  finish: () => finish?.(), succeed: () => { failRead = false; resolveRead?.({ text }) },
  snapshot: () => ({ state: Voice.voiceState(), opens, posts, tracks: tracks.map(track => track.readyState) }),
} })
