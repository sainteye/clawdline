import AVFoundation
import Foundation

/// Dictation through [whisper.cpp](https://github.com/ggml-org/whisper.cpp), for people who
/// speak more than one language in a sentence.
///
/// Apple's recogniser is one language at a time — that is a property of the API, not a setting —
/// so "把那個 webhook 的 retry 改成 exponential backoff" is a sentence it cannot be asked to
/// hear. Whisper transcribes mixed speech as a matter of course, which is the whole reason this
/// exists. It costs a binary, a model file of several hundred megabytes, and the text arriving
/// when you stop talking instead of as you go.
///
/// **Optional on purpose.** Nothing here runs unless a binary and a model are both present, and
/// the app ships neither: a default that needs a download is not a default. See docs/whisper.md.
///
/// Recording and transcription are separate, because whisper.cpp is not a streaming transcriber.
/// Audio is captured while you hold the session open and handed over in one piece at the end —
/// which is also why there are no partial results to show.
final class Whisper {

    private(set) var state: Voice.State = .idle
    var onState: ((Voice.State) -> Void)?
    var onText: ((String) -> Void)?
    var onLevel: ((Float) -> Void)?
    var vocabulary: [String] = []

    private let engine = AVAudioEngine()
    private var samples = Data()
    private let sampleRate: Double = 16_000     // what the model was trained on

    var isListening: Bool { if case .listening = state { return true }; return false }

    // MARK: - Finding the pieces

    /// The `whisper-cli` binary, if this Mac has one.
    ///
    /// Configured path first, then where Homebrew puts it on each architecture, then `PATH`.
    /// Older builds called it `main`, which is why more than one name is tried.
    static func binary(configured: String) -> String? {
        var candidates = configured.isEmpty ? [] : [configured]
        for dir in ["/opt/homebrew/bin", "/usr/local/bin"] {
            candidates += ["\(dir)/whisper-cli", "\(dir)/whisper-cpp", "\(dir)/main"]
        }
        if let found = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) {
            return found
        }
        let out = run("/usr/bin/env", ["which", "whisper-cli"])?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return (out?.isEmpty == false) ? out : nil
    }

    /// The model file. Configured path, else the largest `ggml-*.bin` lying in the usual places —
    /// largest because if somebody downloaded two, the big one is the one they meant to use.
    static func model(configured: String) -> String? {
        if !configured.isEmpty, FileManager.default.isReadableFile(atPath: configured) {
            return configured
        }
        let fm = FileManager.default
        let home = fm.homeDirectoryForCurrentUser
        let places = [
            home.appendingPathComponent(".cache/whisper"),
            home.appendingPathComponent("Library/Application Support/Clawdline/models"),
            home.appendingPathComponent("models"),
        ]
        var best: (path: String, size: Int)?
        for dir in places {
            let names = (try? fm.contentsOfDirectory(atPath: dir.path)) ?? []
            for name in names where name.hasPrefix("ggml-") && name.hasSuffix(".bin") {
                let path = dir.appendingPathComponent(name).path
                let size = ((try? fm.attributesOfItem(atPath: path))?[.size] as? Int) ?? 0
                if best == nil || size > best!.size { best = (path, size) }
            }
        }
        return best?.path
    }

    static func isAvailable(binary configuredBinary: String, model configuredModel: String) -> Bool {
        Self.binary(configured: configuredBinary) != nil && Self.model(configured: configuredModel) != nil
    }

    // MARK: - Listening

    func toggle(locale candidates: [String]) {
        isListening ? stop() : start()
    }

    private func start() {
        guard Self.binary(configured: Config.shared.whisperBinary) != nil,
              Self.model(configured: Config.shared.whisperModel) != nil else {
            fail(L.t.whisperMissing)
            return
        }
        samples = Data()

        let input = engine.inputNode
        let source = input.outputFormat(forBus: 0)
        guard let target = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: sampleRate,
                                         channels: 1, interleaved: true),
              let converter = AVAudioConverter(from: source, to: target) else {
            fail(L.t.voiceUnavailable)
            return
        }

        input.removeTap(onBus: 0)
        input.installTap(onBus: 0, bufferSize: 2048, format: source) { [weak self] buffer, _ in
            guard let self else { return }
            self.report(level: buffer)
            // The model wants 16 kHz mono; the microphone gives whatever it gives.
            let ratio = self.sampleRate / source.sampleRate
            let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 1024
            guard let out = AVAudioPCMBuffer(pcmFormat: target, frameCapacity: capacity) else { return }
            var supplied = false
            var error: NSError?
            converter.convert(to: out, error: &error) { _, status in
                if supplied { status.pointee = .noDataNow; return nil }
                supplied = true
                status.pointee = .haveData
                return buffer
            }
            guard error == nil, let channel = out.int16ChannelData?[0] else { return }
            self.samples.append(UnsafeBufferPointer(start: channel, count: Int(out.frameLength)))
        }

        engine.prepare()
        do { try engine.start() } catch {
            fail(error.localizedDescription)
            return
        }
        set(.listening(onDevice: true))     // it never leaves the machine, that is the point
    }

    func stop() {
        guard isListening else { return }
        engine.inputNode.removeTap(onBus: 0)
        if engine.isRunning { engine.stop() }
        set(.transcribing)

        let audio = samples
        samples = Data()
        let terms = vocabulary
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let text = Self.transcribe(audio, rate: self?.sampleRate ?? 16_000, vocabulary: terms)
            DispatchQueue.main.async {
                guard let self else { return }
                if let text, !text.isEmpty {
                    self.onText?(text)
                    self.set(.idle)
                } else {
                    self.fail(L.t.whisperNothingHeard)
                }
            }
        }
    }

    func forgetAccumulated() {}   // nothing is accumulated: one recording, one result

    // MARK: - Transcribing

    private static func transcribe(_ samples: Data, rate: Double, vocabulary: [String]) -> String? {
        guard samples.count > Int(rate) / 4,          // under a quarter second is a stray click
              let bin = binary(configured: Config.shared.whisperBinary),
              let model = model(configured: Config.shared.whisperModel) else { return nil }

        let wav = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-\(UUID().uuidString).wav")
        guard (try? wavData(samples, rate: rate).write(to: wav)) != nil else { return nil }
        defer { try? FileManager.default.removeItem(at: wav) }

        var args = ["-m", model, "-f", wav.path,
                    "-l", "auto",        // it decides, and it is allowed to change its mind
                    "-nt",               // no timestamps: this is a prompt, not a subtitle file
                    "-np"]               // no progress bar in the output we are about to parse
        // whisper takes a "previous context" string, which is the same lever as Apple's
        // contextual strings — the words to expect, in a sentence it can read.
        if !vocabulary.isEmpty {
            args += ["--prompt", vocabulary.prefix(60).joined(separator: ", ")]
        }
        guard let out = run(bin, args) else { return nil }
        return out
            .split(separator: "\n")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty && !$0.hasPrefix("[") }
            .joined(separator: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// A 44-byte PCM header in front of the samples. Writing it by hand rather than reaching for
    /// AVAudioFile keeps the whole conversion in one place, which is where the bugs would be.
    static func wavData(_ samples: Data, rate: Double) -> Data {
        var out = Data()
        func ascii(_ s: String) { out.append(contentsOf: s.utf8) }
        func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
        func u16(_ v: UInt16) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }

        let channels: UInt16 = 1, bits: UInt16 = 16
        let byteRate = UInt32(rate) * UInt32(channels) * UInt32(bits / 8)
        ascii("RIFF"); u32(UInt32(36 + samples.count)); ascii("WAVE")
        ascii("fmt "); u32(16); u16(1); u16(channels)
        u32(UInt32(rate)); u32(byteRate); u16(channels * bits / 8); u16(bits)
        ascii("data"); u32(UInt32(samples.count))
        out.append(samples)
        return out
    }

    // MARK: - Plumbing

    private func report(level buffer: AVAudioPCMBuffer) {
        guard let channel = buffer.floatChannelData?[0] else { return }
        var sum: Float = 0
        for i in 0..<Int(buffer.frameLength) { sum += channel[i] * channel[i] }
        let rms = sqrt(sum / Float(max(1, buffer.frameLength)))
        let level = min(1, max(0, (20 * log10(max(rms, 1e-7)) + 50) / 50))
        DispatchQueue.main.async { self.onLevel?(level) }
    }

    private func fail(_ message: String) {
        set(.failed(message))
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) { [weak self] in
            if case .failed = self?.state { self?.set(.idle) }
        }
    }

    private func set(_ next: Voice.State) {
        state = next
        onState?(next)
    }

    private static func run(_ launch: String, _ args: [String]) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: launch)
        task.arguments = args
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice
        do { try task.run() } catch { return nil }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitUntilExit()
        return String(data: data, encoding: .utf8)
    }
}
