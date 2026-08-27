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
        /// Recorded, now being turned into text. Only the Whisper engine goes through this:
        /// Apple's is live, so there is nothing to wait for.
        case transcribing
        case failed(String)
    }

    private(set) var state: State = .idle
    var isListening: Bool { if case .listening = state { return true }; return false }
    var onState: ((State) -> Void)?
    /// Called with the text so far, replacing whatever the last call said.
    var onText: ((String) -> Void)?
    /// Loudness, 0…1, roughly once per audio buffer. For showing that it is hearing you.
    var onLevel: ((Float) -> Void)?
    /// Words to bias recognition towards. See `vocabulary(from:extras:)`.
    var vocabulary: [String] = []
    /// Also record, and replace the whole dictated run with Whisper's reading of it when you
    /// stop. Live text while you talk, a better sentence when you finish — the two engines are
    /// good at opposite halves of the same job, so neither has to be chosen over the other.
    var refineWithWhisper = false
    /// Called when a stretch of speech has been fixed and the next one should start after it.
    var onSettled: (() -> Void)?
    /// True while a system permission sheet is up, false once it has been answered.
    ///
    /// The panel hides itself when it stops being the key window, because that is what an app
    /// switch looks like. A permission sheet looks exactly the same from here — so the first
    /// time anyone dictated, the box vanished the moment they were asked, and granting brought
    /// nothing back. Worse, hiding stops the microphone, so the permission they had just given
    /// bought them nothing until they opened it again and asked a second time.
    var onPermissionPrompt: ((Bool) -> Void)?

    /// Built the first time somebody dictates, never before.
    ///
    /// **An object that owns a microphone should not exist before anybody has asked for one.**
    /// This was a stored property, so it was constructed with the panel — which is to say at
    /// launch — and macOS decides for itself when the audio input has been touched closely enough
    /// to be worth asking about. The symptom was the microphone prompt appearing before the
    /// microphone button had ever been pressed, which is exactly the promise `Info.plist` makes
    /// underneath `NSMicrophoneUsageDescription`: *only while you hold a dictation session open*.
    ///
    /// Lazy is the whole fix, and it is the right shape anyway: nothing about this app needs an
    /// audio engine until the moment it is recording.
    private lazy var engine = AVAudioEngine()
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
    /// 16 kHz mono, kept only while refining is on.
    ///
    /// Written from the audio thread and taken from the main one, so every touch goes through
    /// the lock. It did not, and the two threads mutating one `Data` crashed the app inside
    /// `append` — a race, so it happened when a pause happened to land mid-buffer.
    private var recording = Data()
    private let recordingLock = NSLock()
    private var recorder: AVAudioConverter?
    private let recordingRate: Double = 16_000
    private var silence = SilenceDetector()
    private var settling = false

    /// Deciding when somebody has stopped talking.
    ///
    /// Not a fixed threshold. The first version used one — 0.12 on a 0…1 scale — and it never
    /// fired in a real room: measured here, an ordinary quiet office sits at 0.28 with peaks
    /// past 0.7, so "quiet" was never true and nothing ever settled. A number that works in one
    /// room is not a threshold, it is that room.
    ///
    /// The floor is the quietest moment in the last few seconds. Speech has gaps in it — between
    /// syllables, between words — so those gaps are the room, and anything a clear margin above
    /// them is a voice. A version that instead let the floor drift upwards had the opposite bug:
    /// hold a note long enough and it becomes the background.
    struct SilenceDetector {
        private var recent: [Float] = []       // the quietest point in each finished slice
        private var lowestHere: Float = 1
        private var sliceEnds: Double = 0
        private var lastLoud: Double = 0
        /// A settle ends a stretch of speech. Without speech first there is no stretch to end,
        /// so silence on its own fires once at most, not once per gap forever.
        private var armed = false
        /// Whether anything above the floor has happened since the last settle. A stretch of
        /// pure room tone has nothing in it to transcribe, and handing one to Whisper is how
        /// you get a confident short sentence out of an empty room.
        private(set) var heardSpeech = false
        /// The same question asked of the whole session rather than the current stretch.
        ///
        /// A settle clears `heardSpeech` so the next stretch starts empty; ending the session
        /// needs the other answer — a microphone that was opened and never spoken into should
        /// stay open, because nothing has happened yet for a silence to be the end of.
        private(set) var everHeardSpeech = false
        private var lastSpeech: Double = 0

        private let sliceSeconds = 0.5
        private let sliceCount = 6             // three seconds of history
        private let margin: Float = 0.14

        var floor: Float { min(lowestHere, recent.min() ?? 1) }

        /// How long since anything was actually said — across settles, not since the last one.
        func quiet(now: Double) -> Double { everHeardSpeech ? now - lastSpeech : 0 }

        mutating func feed(_ level: Float, now: Double, gap: Double) -> Bool {
            if sliceEnds == 0 {
                sliceEnds = now + sliceSeconds
                lastLoud = now
            }
            lowestHere = min(lowestHere, level)
            if now >= sliceEnds {
                recent.append(lowestHere)
                if recent.count > sliceCount { recent.removeFirst() }
                lowestHere = 1
                sliceEnds = now + sliceSeconds
            }

            if level > floor + margin {
                lastLoud = now
                lastSpeech = now
                armed = true
                heardSpeech = true
                everHeardSpeech = true
                return false
            }
            guard gap > 0, armed, now - lastLoud > gap else { return false }
            armed = false
            return true
        }

        mutating func reset(now: Double) {
            recent = []
            lowestHere = 1
            sliceEnds = 0
            lastLoud = now
            lastSpeech = now
            armed = false
            heardSpeech = false
            everHeardSpeech = false
        }

        /// Called once a stretch has been handed over.
        mutating func startNewStretch(now: Double) {
            lastLoud = now
            armed = false
            heardSpeech = false
        }
    }

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
    static let alwaysExpected = [
        "Clawdline", "Claude", "Claude Code", "Clawd", "Clawdfather",
        "Commit", "Session", "Agent",
    ]

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
        // Two sheets can appear here, speech and then the microphone, and the second only after
        // the first is answered. So the guard covers the whole run up to listening rather than
        // just the call that asks — lifting it between the two would hide the panel in the gap.
        let asking = SFSpeechRecognizer.authorizationStatus() == .notDetermined
            || AVCaptureDevice.authorizationStatus(for: .audio) == .notDetermined
        if asking { onPermissionPrompt?(true) }
        SFSpeechRecognizer.requestAuthorization { [weak self] status in
            DispatchQueue.main.async {
                guard let self else { return }
                guard status == .authorized else {
                    if asking { self.onPermissionPrompt?(false) }
                    self.fail(L.t.voiceNoPermission)
                    return
                }
                // Ask for the microphone before starting the engine. Left to `AVAudioEngine`,
                // the sheet arrives underneath a tap that is already installed, and the first
                // seconds of what you say go into a stream nobody is allowed to read yet.
                AVCaptureDevice.requestAccess(for: .audio) { granted in
                    DispatchQueue.main.async {
                        if asking { self.onPermissionPrompt?(false) }
                        guard granted else {
                            self.fail(L.t.voiceNoPermission)
                            return
                        }
                        self.listen(locale: choice.locale, onDevice: choice.onDevice)
                    }
                }
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
        let source = input.outputFormat(forBus: 0)
        _ = takeRecording()
        recorder = nil
        if refineWithWhisper,
           let target = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: recordingRate,
                                      channels: 1, interleaved: true) {
            recorder = AVAudioConverter(from: source, to: target)
        }
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
            DispatchQueue.main.async {
                self?.onLevel?(level)
                self?.noteLevel(level)
            }
            self?.record(buffer)
        }
        tapped = true
        engine.prepare()
        do {
            try engine.start()
        } catch {
            fail(error.localizedDescription)
            return
        }

        settled = ""
        latest = ""
        lastEmitted = ""
        silence.reset(now: CFAbsoluteTimeGetCurrent())
        settling = false
        listen(on: request, with: recogniser, onDevice: onDevice)
        refinedOnDevice = onDevice
        set(.listening(onDevice: onDevice))
    }

    /// Attach a task to a request, and keep the session going across the pauses.
    private func listen(on request: SFSpeechAudioBufferRecognitionRequest,
                        with recogniser: SFSpeechRecognizer, onDevice: Bool) {
        task = recogniser.recognitionTask(with: request) { [weak self] result, error in
            guard let self else { return }
            // Once we have stopped, Apple's task can still deliver — cancelling one is a request,
            // not an instruction. Its parting result was landing after the microphone closed and
            // emptying the box, which is why the words vanished for a second before Whisper's
            // version appeared.
            guard case .listening = self.state else { return }
            if let result {
                let text = result.bestTranscription.formattedString
                if result.isFinal {
                    // Sentence done. Keep it, and open a new one — the microphone is still on,
                    // and the user said nothing about being finished.
                    self.settled = Self.join(self.settled, text)
                    self.latest = ""
                    self.emit(self.settled)
                    if case .listening = self.state { self.restart(onDevice: onDevice) }
                    return
                }
                // A partial normally grows. One that collapses is the recogniser having moved on
                // to a new utterance without saying so, and the old one is worth keeping.
                if !self.latest.isEmpty, text.count * 2 < self.latest.count {
                    self.settled = Self.join(self.settled, self.latest)
                }
                self.latest = text
                self.emit(Self.join(self.settled, text))
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

    /// Everything on its way to the box goes through here, so "no Simplified" is one rule in
    /// one place rather than a promise each path has to remember to keep.
    private func emit(_ text: String) {
        let script = Whisper.wantsTraditional(Config.shared.voiceLanguage)
            ? Whisper.toTraditional(text) : text
        // Both engines, not just Whisper: Apple is told to expect these words and still mishears
        // them, and doing it in one place is what stops the two paths drifting into disagreeing
        // about how your own project is spelled.
        let named = Whisper.applyVocabulary(Whisper.tidy(script),
                                            terms: Self.alwaysExpected + Config.shared.voiceVocabulary)
        let out = named
        // Kept because how a sentence ended is evidence about whether it ended. See `stopDelay`.
        if !out.isEmpty { lastEmitted = out }
        onText?(out)
    }

    /// The last thing put in the box, whichever engine wrote it.
    private var lastEmitted = ""

    /// Sentences need a space between them; a language that does not use spaces does not.
    static func join(_ first: String, _ second: String) -> String {
        guard !first.isEmpty else { return second }
        guard !second.isEmpty else { return first }
        let needsSpace = first.last?.isASCII == true && second.first?.isASCII == true
        return first + (needsSpace ? " " : "") + second
    }

    /// Forget the words already handed over.
    ///
    /// Called when the user has edited what was dictated: those words are theirs now, and
    /// sending them again would insert a second copy next to the version they just fixed.
    /// Hand the recording over and start a new one, without either thread seeing a half state.
    private func takeRecording() -> Data {
        recordingLock.lock()
        defer { recordingLock.unlock() }
        let out = recording
        recording = Data()
        return out
    }

    func forgetAccumulated() {
        settled = ""
        latest = ""
        // The audio recorded so far belongs to text the user has already taken ownership of.
        // Handing it to Whisper afterwards would write that sentence a second time.
        _ = takeRecording()
    }

    /// Watch for the gap between sentences.
    ///
    /// Speech sits well above this; a quiet room sits well below. The threshold does not have to
    /// be clever because the thing being detected is a person stopping, not a signal ending.
    private func noteLevel(_ level: Float) {
        let now = CFAbsoluteTimeGetCurrent()
        let hit = silence.feed(level, now: now, gap: Config.shared.voiceSettleSeconds)
        // Loudness is tracked whatever the state, and only acted on while listening. Talking
        // over the second pass is still talking: if those seconds did not count, a long stretch
        // would end the session the instant Whisper handed its version back.
        guard case .listening = state, !settling else { return }

        recordingLock.lock()
        let recorded = !recording.isEmpty
        recordingLock.unlock()
        if hit, recorded || !latest.isEmpty { settle(); return }

        // A pause ends a sentence. A longer one ends the session — which is the whole point:
        // finishing a thought should not also mean reaching for the key that turns the
        // microphone off.
        let base = Config.shared.voiceStopSeconds
        guard base > 0, silence.everHeardSpeech,
              silence.quiet(now: now) > Self.stopDelay(base: base, after: lastEmitted) else { return }
        Log.write("voice: stopped itself after a long silence")
        stop()
    }

    /// How long a silence has to run before it ends the session rather than a sentence.
    ///
    /// A sentence that arrived with a full stop on it is one somebody finished. One that breaks
    /// off mid-clause is somebody thinking, and cutting them off there costs exactly what this
    /// is meant to save — reaching for the key again, only now by surprise. So the unfinished
    /// case waits longer rather than never firing: being wrong in that direction is free.
    static let unfinishedFactor = 1.75

    static func stopDelay(base: Double, after text: String) -> Double {
        guard base > 0 else { return 0 }
        // Closing marks belong to the sentence they close; the full stop is in front of them.
        let closers = CharacterSet(charactersIn: " \t\n」』】〕）\")]}")
        guard let last = text.trimmingCharacters(in: closers).last else { return base }
        return ".!?。！？…‽".contains(last) ? base : base * unfinishedFactor
    }

    /// Fix everything said so far and start again after it.
    ///
    /// Whisper reads only this stretch rather than the whole session, which is both faster and
    /// the reason the text you already read stops moving.
    private func settle() {
        settling = true
        let spoken = silence.heardSpeech
        silence.startNewStretch(now: CFAbsoluteTimeGetCurrent())
        let audio = takeRecording()
        forgetAccumulated()

        func done() {
            self.onSettled?()
            self.settling = false
            // Somebody pressed stop while this was running. Go back to listening for exactly as
            // long as it takes `finish` to read what was said in the meantime.
            if self.stopAfterSettling {
                self.stopAfterSettling = false
                self.set(.listening(onDevice: self.refinedOnDevice))
                self.finish()
                return
            }
            if case .transcribing = self.state { self.set(.listening(onDevice: refinedOnDevice)) }
        }

        // Nothing was said in that stretch. Sending room tone to a model that always answers
        // is how you get "Thank you." appearing in an empty box.
        guard spoken, refineWithWhisper, audio.count > Int(recordingRate) / 4 else {
            done()
            return
        }
        set(.transcribing)
        let terms = vocabulary
        let rate = recordingRate
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let seconds = Double(audio.count) / (rate * 2)
            let better = Whisper.transcribe(audio, rate: rate, vocabulary: terms,
                                            language: Config.shared.voiceLanguage)
            // Whether the second pass ran, and what it made of it. Without this the only
            // evidence is the text itself, and "Apple's version was never replaced" looks
            // exactly like "Whisper heard it that way".
            Log.write(String(format: "voice: whisper read %.1fs → %@", seconds,
                             better.map { String($0.prefix(48)) } ?? "nothing"))
            DispatchQueue.main.async {
                guard let self else { return }
                if let better, !better.isEmpty { self.emit(better) }
                done()
            }
        }
    }

    /// Remembered so a settle can put the listening state back the way it found it.
    private var refinedOnDevice = true

    /// Convert a buffer to what the model wants and keep it. Same audio, second purpose.
    private func record(_ buffer: AVAudioPCMBuffer) {
        guard let recorder else { return }
        let ratio = recordingRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 1024
        guard let out = AVAudioPCMBuffer(pcmFormat: recorder.outputFormat,
                                         frameCapacity: capacity) else { return }
        var supplied = false
        var error: NSError?
        recorder.convert(to: out, error: &error) { _, status in
            if supplied { status.pointee = .noDataNow; return nil }
            supplied = true
            status.pointee = .haveData
            return buffer
        }
        guard error == nil, let channel = out.int16ChannelData?[0] else { return }
        recordingLock.lock()
        recording.append(UnsafeBufferPointer(start: channel, count: Int(out.frameLength)))
        recordingLock.unlock()
    }

    /// Whether there is a tap on the input to take off again.
    ///
    /// **`engine.inputNode` is not a getter.** Reaching for it makes AVAudioEngine build the
    /// input node, which allocates render resources against the audio HAL — synchronously, on
    /// whatever thread asked, and with no timeout. `stop()` runs every time the panel closes,
    /// dictation or no dictation, so on a machine whose HAL had wedged the whole app stopped
    /// with it: the main thread sat inside `AudioUnitInitialize` and never came out, and what
    /// that looks like from the outside is an application that has simply stopped answering.
    /// Seen twice on 2026-08-24, both times after a `clawdline://snapshot`, whose last act is to
    /// put the panel away.
    ///
    /// So the node is only reached for when something was actually put on it.
    private var tapped = false

    /// A stop arrived while a settle was still being transcribed. See `stop()`.
    private var stopAfterSettling = false

    func stop() {
        // See `tapped`. `isRunning` is a plain state read and safe either way; the node itself
        // is only asked for when this session put something on it.
        if tapped {
            engine.inputNode.removeTap(onBus: 0)
            tapped = false
        }
        if engine.isRunning { engine.stop() }
        request?.endAudio()
        task?.cancel()
        task = nil
        request = nil

        switch state {
        case .idle, .failed:
            return
        case .transcribing:
            // A pass is running. If it is a settle, everything said since it started is sitting
            // in the recording and used to be dropped right here — the microphone closed, this
            // returned, and that stretch was never read. Wait for the pass and finish then.
            if settling { stopAfterSettling = true }
            return
        case .listening:
            finish()
        }
    }

    private func finish() {
        // Nothing to improve on, or nothing installed to improve it with.
        let spoken = silence.heardSpeech
        let audio = takeRecording()
        guard spoken, refineWithWhisper, audio.count > Int(recordingRate) / 4 else {
            set(.idle)
            return
        }

        set(.transcribing)
        let terms = vocabulary
        let rate = recordingRate
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let seconds = Double(audio.count) / (rate * 2)
            let better = Whisper.transcribe(audio, rate: rate, vocabulary: terms,
                                            language: Config.shared.voiceLanguage)
            // Whether the second pass ran, and what it made of it. Without this the only
            // evidence is the text itself, and "Apple's version was never replaced" looks
            // exactly like "Whisper heard it that way".
            Log.write(String(format: "voice: whisper read %.1fs → %@", seconds,
                             better.map { String($0.prefix(48)) } ?? "nothing"))
            DispatchQueue.main.async {
                guard let self else { return }
                // Only replace it if there is something to replace it with. A failed run should
                // leave the live text alone, not empty the box you were about to send.
                if let better, !better.isEmpty { self.emit(better) }
                self.set(.idle)
            }
        }
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
