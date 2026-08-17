import AVFoundation
import Speech

/// Talking into the bar instead of typing into it.
///
/// Apple's Speech framework, **on-device when the machine can, over the network when it
/// cannot** — and it says which, every time, while it is listening.
///
/// Whether it can run locally is a fact about the machine rather than the language: macOS
/// recognises offline only in the dictation languages that have been downloaded. So the
/// difference is not "supported" versus "unsupported", it is "this Mac has that one installed"
/// versus "this one goes to Apple to be transcribed". Both are usable; only one of them is
/// something you would want to find out afterwards, which is why the bar says so at the time.
///
/// The undocumented `startDictation:` on NSApplication would have been three lines and no
/// permissions, and it is what the Fn-Fn shortcut calls. It is private, so it can stop working
/// in a system update with no warning and nothing to catch it — and the text would land
/// wherever the system decided, not necessarily in this box.
final class Voice {

    enum State: Equatable {
        case idle
        /// `onDevice` is false when the audio is going to Apple to be transcribed.
        case listening(onDevice: Bool)
        case failed(String)
    }

    private(set) var state: State = .idle
    var onState: ((State) -> Void)?
    /// Called with the text so far, replacing whatever the last call said.
    var onText: ((String) -> Void)?
    /// Loudness, 0…1, roughly once per audio buffer. For showing that it is hearing you.
    var onLevel: ((Float) -> Void)?
    /// Words to bias recognition towards. See `vocabulary(from:extras:)`.
    var vocabulary: [String] = []

    private let engine = AVAudioEngine()
    private var recogniser: SFSpeechRecognizer?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    /// Utterances the recogniser has finished with this session.
    ///
    /// It settles a sentence at a pause and starts the next one from nothing, so
    /// `bestTranscription` is only ever the sentence in progress. Emitting that alone made the
    /// second thing you said delete the first.
    private var settled = ""
    private var latest = ""

    /// The locale to listen in, and whether it can be done without leaving the machine.
    ///
    /// The first candidate that works wins. On-device is reported, never preferred: an earlier
    /// version let it decide, and asking for English on a Mac with only Chinese installed got
    /// you a recogniser listening in Chinese. Which language you are speaking is not something
    /// to trade away for privacy — the trade on offer is where it gets transcribed, and that
    /// answer is on screen the whole time it listens.
    static func pick(_ candidates: [String] = []) -> (locale: Locale, onDevice: Bool)? {
        for tag in candidates + [Locale.current.identifier, "en-US"] {
            let locale = Locale(identifier: tag)
            guard let r = SFSpeechRecognizer(locale: locale), r.isAvailable else { continue }
            return (locale, r.supportsOnDeviceRecognition)
        }
        return nil
    }

    /// Names this app cannot be expected to guess and you will certainly say.
    ///
    /// "Clawdline" is not a word in any language model's vocabulary, and it is the single most
    /// likely thing to be said to a bar called Clawdline. The rest are what you are talking to.
    static let alwaysExpected = ["Clawdline", "Claude", "Claude Code", "Clawd"]

    /// Words to tip the scales with, out of what you have already typed.
    ///
    /// Neither of Apple's speech APIs switches language mid-sentence — one recogniser, one
    /// locale — so "說一句中文然後 commit 一下" is not a mode you can turn on. What is available
    /// is `contextualStrings`: a hundred phrases the model is told to expect. Latin words are
    /// exactly the ones a Chinese recogniser guesses at, so those are what get sent.
    ///
    /// Drawn from your own prompt history, because the words you type at Claude Code are the
    /// words you would say to it. It gets better the more you use it, and it needs no list to
    /// maintain — a vocabulary you have to curate is one that goes stale the week you write it.
    static func vocabulary(from history: [String], extras: [String] = [], limit: Int = 100) -> [String] {
        var seen = Set<String>()
        var out: [String] = []

        func add(_ word: String) {
            let w = word.trimmingCharacters(in: CharacterSet(charactersIn: "`'\"(),.:;!?[]{}"))
            guard w.count > 2, w.count < 40, !seen.contains(w.lowercased()) else { return }
            // Latin only. A Chinese term the recogniser already knows gains nothing from being
            // listed, and the budget is a hundred.
            guard w.unicodeScalars.allSatisfy({ $0.isASCII }) else { return }
            guard w.rangeOfCharacter(from: .letters) != nil else { return }
            seen.insert(w.lowercased())
            out.append(w)
        }

        extras.forEach(add)
        // Newest first: what you said an hour ago beats what you said last month.
        for line in history.reversed() {
            for word in line.split(whereSeparator: { $0.isWhitespace }) { add(String(word)) }
            if out.count >= limit { break }
        }
        return Array(out.prefix(limit))
    }

    func toggle(locale candidates: [String]) {
        if case .listening = state { stop() } else { start(locale: candidates) }
    }

    private func start(locale candidates: [String]) {
        guard let choice = Self.pick(candidates) else {
            fail(L.t.voiceUnavailable)
            return
        }
        SFSpeechRecognizer.requestAuthorization { [weak self] status in
            DispatchQueue.main.async {
                guard let self else { return }
                guard status == .authorized else {
                    self.fail(L.t.voiceNoPermission)
                    return
                }
                self.listen(locale: choice.locale, onDevice: choice.onDevice)
            }
        }
    }

    private func listen(locale: Locale, onDevice: Bool) {
        guard let recogniser = SFSpeechRecognizer(locale: locale), recogniser.isAvailable else {
            fail(L.t.voiceUnavailable)
            return
        }
        self.recogniser = recogniser

        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        // Ask for local when local exists. Left false, the framework decides for itself, and
        // an installed language would still sometimes go out over the network.
        request.requiresOnDeviceRecognition = onDevice
        request.contextualStrings = vocabulary
        request.addsPunctuation = true
        self.request = request

        let input = engine.inputNode
        input.removeTap(onBus: 0)
        // Appends to whichever request is current, not the one captured here: a pause swaps in
        // a new one and the audio has to follow it.
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { [weak self] buffer, _ in
            self?.request?.append(buffer)
            guard let channel = buffer.floatChannelData?[0] else { return }
            var sum: Float = 0
            for i in 0..<Int(buffer.frameLength) { sum += channel[i] * channel[i] }
            let rms = sqrt(sum / Float(max(1, buffer.frameLength)))
            // Loudness is logarithmic; a linear meter sits near zero and then jumps.
            let level = min(1, max(0, (20 * log10(max(rms, 1e-7)) + 50) / 50))
            DispatchQueue.main.async { self?.onLevel?(level) }
        }
        engine.prepare()
        do {
            try engine.start()
        } catch {
            fail(error.localizedDescription)
            return
        }

        settled = ""
        latest = ""
        listen(on: request, with: recogniser, onDevice: onDevice)
        set(.listening(onDevice: onDevice))
    }

    /// Attach a task to a request, and keep the session going across the pauses.
    private func listen(on request: SFSpeechAudioBufferRecognitionRequest,
                        with recogniser: SFSpeechRecognizer, onDevice: Bool) {
        task = recogniser.recognitionTask(with: request) { [weak self] result, error in
            guard let self else { return }
            if let result {
                let text = result.bestTranscription.formattedString
                if result.isFinal {
                    // Sentence done. Keep it, and open a new one — the microphone is still on,
                    // and the user said nothing about being finished.
                    self.settled = Self.join(self.settled, text)
                    self.latest = ""
                    self.onText?(self.settled)
                    if case .listening = self.state { self.restart(onDevice: onDevice) }
                    return
                }
                // A partial normally grows. One that collapses is the recogniser having moved on
                // to a new utterance without saying so, and the old one is worth keeping.
                if !self.latest.isEmpty, text.count * 2 < self.latest.count {
                    self.settled = Self.join(self.settled, self.latest)
                }
                self.latest = text
                self.onText?(Self.join(self.settled, text))
            }
            if error != nil { self.stop() }
        }
    }

    private func restart(onDevice: Bool) {
        guard let recogniser else { return }
        task?.cancel()
        let next = SFSpeechAudioBufferRecognitionRequest()
        next.shouldReportPartialResults = true
        next.requiresOnDeviceRecognition = onDevice
        next.contextualStrings = vocabulary
        next.addsPunctuation = true
        request = next
        listen(on: next, with: recogniser, onDevice: onDevice)
    }

    /// Sentences need a space between them; a language that does not use spaces does not.
    static func join(_ first: String, _ second: String) -> String {
        guard !first.isEmpty else { return second }
        guard !second.isEmpty else { return first }
        let needsSpace = first.last?.isASCII == true && second.first?.isASCII == true
        return first + (needsSpace ? " " : "") + second
    }

    func stop() {
        engine.inputNode.removeTap(onBus: 0)
        if engine.isRunning { engine.stop() }
        request?.endAudio()
        task?.cancel()
        task = nil
        request = nil
        if case .listening = state { set(.idle) }
    }

    private func fail(_ message: String) {
        stop()
        set(.failed(message))
        // Failures are worth saying once, not wearing as a state.
        DispatchQueue.main.asyncAfter(deadline: .now() + 2.5) { [weak self] in
            if case .failed = self?.state { self?.set(.idle) }
        }
    }

    private func set(_ next: State) {
        state = next
        onState?(next)
    }
}
