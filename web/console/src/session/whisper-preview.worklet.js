// 16 kHz mono PCM. The first window is shorter so a short dictation can
// show text before Stop. Later windows still give Whisper three seconds.
const rate = 16000
const windowSamples = 3 * rate

class WhisperPreviewCapture extends AudioWorkletProcessor {
  constructor() {
    super()
    this.phase = 0
    this.sum = 0
    this.count = 0
    this.window = new Int16Array(windowSamples)
    this.offset = 0
    this.first = true
    this.hasSignal = false
  }

  process(inputs) {
    const mono = inputs[0]?.[0]
    if (!mono) return true
    for (const sample of mono) {
      this.sum += sample
      this.count += 1
      this.phase += rate
      if (this.phase < sampleRate) continue
      this.phase -= sampleRate
      const averaged = Math.max(-1, Math.min(1, this.sum / this.count))
      this.window[this.offset++] = averaged < 0 ? averaged * 32768 : averaged * 32767
      this.hasSignal ||= this.window[this.offset - 1] !== 0
      this.sum = 0
      this.count = 0
      if (this.offset !== (this.first ? 2 * rate : windowSamples)) continue
      if (this.hasSignal) {
        const audio = this.first ? this.window.slice(0, this.offset).buffer : this.window.buffer
        this.port.postMessage(audio, [audio])
        if (!this.first) this.window = new Int16Array(windowSamples)
      }
      this.first = false
      this.hasSignal = false
      this.offset = 0
    }
    return true
  }
}

registerProcessor("whisper-preview", WhisperPreviewCapture)
