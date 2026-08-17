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

    private let engine = AVAudioEngine()
    private var recogniser: SFSpeechRecognizer?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?

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
        self.request = request

        let input = engine.inputNode
        input.removeTap(onBus: 0)
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { buffer, _ in
            request.append(buffer)
        }
        engine.prepare()
        do {
            try engine.start()
        } catch {
            fail(error.localizedDescription)
            return
        }

        task = recogniser.recognitionTask(with: request) { [weak self] result, error in
            guard let self else { return }
            if let result {
                self.onText?(result.bestTranscription.formattedString)
            }
            if error != nil || result?.isFinal == true {
                self.stop()
            }
        }
        set(.listening(onDevice: onDevice))
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
