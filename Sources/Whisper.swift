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
/// This is only the transcriber. The microphone, the conversion and the recording live in
/// `Voice`, which needs them anyway — running two audio engines against one microphone to have
/// two opinions about it would be a strange way to get one sentence.
enum Whisper {

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
            for name in names where name.hasPrefix("ggml-") && name.hasSuffix(".bin")
                                    && !name.contains("for-tests") {
                let path = dir.appendingPathComponent(name).path
                let size = ((try? fm.attributesOfItem(atPath: path))?[.size] as? Int) ?? 0
                if best == nil || size > best!.size { best = (path, size) }
            }
        }
        return best?.path
    }

    static func isAvailable(binary configuredBinary: String, model configuredModel: String) -> Bool {
        if case .ready = status(binary: configuredBinary, model: configuredModel) { return true }
        return false
    }

    /// Why the second pass is or is not going to happen.
    ///
    /// Separate from a yes/no because the interesting case is neither: `brew install whisper-cpp`
    /// gives you the binary and no model — it ships one called `for-tests-ggml-tiny.bin`, which
    /// is a fixture, not a model — so the commonest state is half-installed. Told "off", you go
    /// and check the thing you already did.
    enum Status: Equatable {
        case ready(model: String)
        case noBinary
        case noModel
    }

    static func status(binary configuredBinary: String, model configuredModel: String) -> Status {
        guard Self.binary(configured: configuredBinary) != nil else { return .noBinary }
        guard let model = Self.model(configured: configuredModel) else { return .noModel }
        return .ready(model: (model as NSString).lastPathComponent)
    }

    // MARK: - Transcribing

    /// What to pass whisper for a locale, and what to seed it with.
    ///
    /// Two separate jobs. `-l zh` picks the language; it does **not** pick the script, and
    /// Whisper's Chinese comes out Simplified by default. The initial prompt does that: a few
    /// words in the script you want, and the rest follows them. Undocumented, widely used, and
    /// the only lever there is short of converting afterwards.
    /// One punctuated sentence per language, used as the prompt.
    ///
    /// The prompt is a writing sample and the transcript imitates it, so a sample with commas
    /// and full stops in it produces them — measured, see `transcribe`. It says nothing in
    /// particular on purpose: what is being shown is the punctuation, not the subject.
    ///
    /// This started out Chinese-only, which meant a fix found by measuring a Chinese clip
    /// reached nobody else's language: everyone dictating in English got whatever punctuation
    /// the model felt like. Adding a language here is one line, and the only qualification
    /// needed is speaking it.
    ///
    /// Verified by measurement in `zh`. The rest are ordinary sentences that a native speaker
    /// is very welcome to improve — a wrong-sounding sample is a worse sample, not a broken one.
    static let seeds: [String: String] = [
        "zh-Hant": "好的，我知道了。那就先這樣，等一下再說。",
        "zh-Hans": "好的，我知道了。那就先这样，等一下再说。",
        "en": "Okay, got it. Let's leave it there for now, and pick it up later.",
        "ja": "はい、わかりました。ひとまずこれで、あとでまた話しましょう。",
        "ko": "네, 알겠습니다. 일단 여기까지 하고, 나중에 다시 이야기하죠.",
        "es": "De acuerdo, entendido. Lo dejamos aquí por ahora y seguimos luego.",
        "pt": "Certo, entendi. Vamos parar por aqui e continuamos depois.",
        "fr": "D'accord, c'est noté. On en reste là pour l'instant, on reprendra plus tard.",
        "de": "Alles klar, verstanden. Wir lassen es vorerst so und machen später weiter.",
        "it": "Va bene, capito. Per ora ci fermiamo qui e riprendiamo dopo.",
        "ru": "Хорошо, понял. Пока остановимся на этом, продолжим позже.",
    ]

    static func language(for tag: String) -> (code: String, seed: String?) {
        let lower = tag.lowercased()
        guard lower != "auto", !lower.isEmpty else { return ("auto", nil) }
        if lower.hasPrefix("zh") {
            // Which script, not which country: the seed is the only lever on that, short of
            // converting afterwards.
            let traditional = lower.contains("tw") || lower.contains("hk")
                || lower.contains("hant") || lower.contains("mo")
            return ("zh", seeds[traditional ? "zh-Hant" : "zh-Hans"])
        }
        let code = String(lower.prefix(2))
        return (code, seeds[code])
    }

    static func transcribe(_ samples: Data, rate: Double, vocabulary: [String],
                           language tag: String) -> String? {
        guard samples.count > Int(rate) / 4,          // under a quarter second is a stray click
              let bin = binary(configured: Config.shared.whisperBinary),
              let model = model(configured: Config.shared.whisperModel) else { return nil }

        // Hand over the part with speech in it, and nothing else.
        let speech = trimSilence(samples, rate: rate)
        guard speech.count > Int(rate) / 4 else { return nil }

        let wav = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-\(UUID().uuidString).wav")
        guard (try? wavData(speech, rate: rate).write(to: wav)) != nil else { return nil }
        defer { try? FileManager.default.removeItem(at: wav) }

        let language = Self.language(for: tag)
        var args = ["-m", model, "-f", wav.path,
                    "-l", language.code,
                    "-nt",               // no timestamps: this is a prompt, not a subtitle file
                    "-np",               // no progress bar in the output we are about to parse
                    "-sns",              // drop non-speech tokens rather than writing them down
                    "-nth", "0.8"]       // and be harder to convince that room tone was a word
        // whisper takes a "previous context" string, which is the same lever as Apple's
        // contextual strings — the words to expect, in a sentence it can read. The script seed
        // rides along in front of it.
        // The seed, and nothing else. **No vocabulary.**
        //
        // Measured on one clip, changing only the prompt:
        //   seed alone                     → 更有結構化。我們需要有目錄…在哪裡。   punctuated
        //   seed + twenty bare words       → 更有結構化我們需要有目錄…在哪裡       none at all
        //   seed + those words in a sentence → still none, once the list got long
        //
        // The prompt is a sample of the text that came before, so a prompt that is mostly a word
        // list produces a transcript with no punctuation in it — and punctuation is worth more
        // here than a nudge towards spellings the model is already good at. Apple's recogniser
        // keeps the vocabulary: `contextualStrings` is a proper list field, not a writing sample,
        // so there it costs nothing.
        if let seed = language.seed { args += ["--prompt", seed] }
        guard let out = run(bin, args) else { return nil }
        let text = out
            .split(separator: "\n")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty && !$0.hasPrefix("[") }
            .joined(separator: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        // A groove is not a sentence. Returning nothing leaves the live text standing, which is
        // the right outcome: it was at least something the recogniser heard.
        guard !looksLikeLoop(text) else { return nil }
        return tidy(wantsTraditional(tag) ? toTraditional(text) : text)
    }

    /// Turn any Simplified characters into Traditional ones, leaving everything else alone.
    ///
    /// The initial prompt only *biases* the script; asking for Traditional and getting a
    /// sentence of Simplified back is a thing that happens. This is the guarantee — macOS ships
    /// ICU's transliterator, so it is a table lookup rather than a model's opinion, and English
    /// in the middle of a sentence passes through untouched.
    ///
    /// It converts characters, not vocabulary: 网络 becomes 網絡 and not 網路. Wording is a
    /// regional choice and this is not the layer that can make it.
    static func toTraditional(_ text: String) -> String {
        guard text.unicodeScalars.contains(where: { $0.value >= 0x4E00 && $0.value <= 0x9FFF })
        else { return text }
        let out = NSMutableString(string: text)
        guard CFStringTransform(out as CFMutableString, nil,
                                "Simplified-Traditional" as CFString, false) else { return text }
        return out as String
    }

    /// Put the spaces and the punctuation back.
    ///
    /// Whisper writes Chinese without a space before the English inside it — 那個webhook的retry
    /// — and reaches for a half-width comma in the middle of a Chinese sentence. Neither is
    /// something the model can be asked to stop doing; both are mechanical to repair.
    ///
    /// A space between Han characters and Latin ones is a typographic convention old enough to
    /// have a name, and it is the difference between a sentence you read and one you decode.
    static func tidy(_ text: String) -> String {
        let chars = Array(text)
        var out = ""
        for (i, ch) in chars.enumerated() {
            let before = i > 0 ? chars[i - 1] : nil
            let after = i + 1 < chars.count ? chars[i + 1] : nil

            if let p = before {
                // Only between the two scripts, and never doubling a space already there.
                let boundary = (isHan(p) && isLatin(ch)) || (isLatin(p) && isHan(ch))
                if boundary, !p.isWhitespace, !ch.isWhitespace { out.append(" ") }
            }

            // A comma with Chinese on either side belongs to the Chinese sentence, even when the
            // word in front of it happens to be English — which is most of them here.
            let touchesHan = (before.map(isHan) ?? false) || (after.map(isHan) ?? false)
            if touchesHan, let full = fullWidth[ch] {
                out.append(full)
            } else {
                out.append(ch)
            }
        }
        return out
    }

    private static let fullWidth: [Character: Character] = [
        ",": "，", ";": "；", ":": "：", "?": "？", "!": "！",
    ]

    // MARK: - Names a transcriber cannot be expected to know

    /// Put back the words that are not words.
    ///
    /// "Clawdline" is in no model's vocabulary and "Claude Code" is two ordinary English words
    /// arranged in a way no corpus has seen, so both come back as something that sounds right:
    /// *cloud code*, *clawed line*. Asking harder does not fix it. Apple's recogniser takes a
    /// list — `contextualStrings` — and costs nothing for it, but the words you finally read
    /// come from Whisper, and Whisper's only lever is `--prompt`.
    ///
    /// **Which is why this is not done there.** `--prompt` is a sample of the text that came
    /// before, not a list of terms, and the transcript imitates it: measured on one clip,
    /// changing nothing else, a prompt of seed-plus-twenty-words produced a transcript with not
    /// one punctuation mark in it. Spelling is worth less than punctuation, and this repair is
    /// mechanical anyway — the same trade as the space between two scripts.
    ///
    /// Conservative on purpose. A term shorter than four letters is matched exactly or not at
    /// all, because at that length everything is within one edit of everything.
    static func applyVocabulary(_ text: String, terms: [String]) -> String {
        guard !text.isEmpty else { return text }
        let words = wordRanges(in: text)
        guard !words.isEmpty else { return text }

        // Longest first, so "Claude Code" is settled before "Claude" can claim half of it.
        let wanted = terms
            .map { (term: $0, key: normalise($0)) }
            .filter { $0.key.count >= 4 }
            .sorted { $0.key.count > $1.key.count }

        var replacements: [(range: Range<String.Index>, with: String)] = []
        var claimed = Set<Int>()

        for (term, key) in wanted {
            // How wrong a word is allowed to be and still count. Calibrated against the two
            // failures that matter, in both directions: "cloudline" is two edits from
            // "clawdline" and has to match, and "claw" is one edit from "Clawd" and must not.
            // So a short term buys no tolerance at all — at five letters, one edit reaches every
            // ordinary word nearby.
            let tolerance = key.count >= 9 ? 2 : (key.count >= 6 ? 1 : 0)
            let span = term.split(separator: " ").count
            for size in 1...(span + 1) {
                guard size <= words.count else { break }
                for start in 0...(words.count - size) {
                    let indices = Array(start..<(start + size))
                    if indices.contains(where: claimed.contains) { continue }
                    let joined = indices.map { normalise(String(text[words[$0]])) }.joined()
                    // Nothing to do when it is already right, and a bad idea to try: an exact
                    // match differing only in case is usually the user's own capitalisation.
                    guard joined != key, !joined.isEmpty else { continue }
                    guard abs(joined.count - key.count) <= tolerance,
                          distance(joined, key) <= tolerance else { continue }
                    indices.forEach { claimed.insert($0) }
                    replacements.append((words[start].lowerBound..<words[start + size - 1].upperBound,
                                         term))
                }
            }
        }

        var out = text
        for r in replacements.sorted(by: { $0.range.lowerBound > $1.range.lowerBound }) {
            out.replaceSubrange(r.range, with: r.with)
        }
        return out
    }

    /// Runs of letters and digits. Everything between them — spaces, punctuation, Han
    /// characters — is a boundary, which is what makes this work inside a Chinese sentence.
    static func wordRanges(in text: String) -> [Range<String.Index>] {
        var out: [Range<String.Index>] = []
        var start: String.Index?
        for i in text.indices {
            let latin = text[i].isASCII && (text[i].isLetter || text[i].isNumber)
            if latin, start == nil { start = i }
            if !latin, let s = start { out.append(s..<i); start = nil }
        }
        if let s = start { out.append(s..<text.endIndex) }
        return out
    }

    private static func normalise(_ s: String) -> String {
        String(s.lowercased().unicodeScalars.filter { CharacterSet.alphanumerics.contains($0) })
    }

    /// Levenshtein, on strings a few characters long. Two rows rather than a matrix, because
    /// this runs on every partial result while somebody is still talking.
    static func distance(_ a: String, _ b: String) -> Int {
        let x = Array(a), y = Array(b)
        if x.isEmpty { return y.count }
        if y.isEmpty { return x.count }
        var prev = Array(0...y.count)
        var row = [Int](repeating: 0, count: y.count + 1)
        for i in 1...x.count {
            row[0] = i
            for j in 1...y.count {
                row[j] = x[i - 1] == y[j - 1]
                    ? prev[j - 1]
                    : min(prev[j - 1], prev[j], row[j - 1]) + 1
            }
            prev = row
        }
        return prev[y.count]
    }

    private static func isHan(_ ch: Character) -> Bool {
        guard let v = ch.unicodeScalars.first?.value else { return false }
        return (0x4E00...0x9FFF).contains(v) || (0x3400...0x4DBF).contains(v)
            || (0xF900...0xFAFF).contains(v)
    }

    private static func isLatin(_ ch: Character) -> Bool {
        guard let v = ch.unicodeScalars.first?.value, v < 0x2E80 else { return false }
        return ch.isLetter || ch.isNumber
    }

    /// Whether this language tag asks for Traditional characters.
    static func wantsTraditional(_ tag: String) -> Bool {
        let lower = tag.lowercased()
        guard lower.hasPrefix("zh") else { return false }
        return lower.contains("tw") || lower.contains("hk") || lower.contains("hant")
            || lower.contains("mo")
    }

    /// Cut the quiet off both ends.
    ///
    /// Whisper does not answer "there was nothing there". Given silence it continues whatever
    /// context it has — which means it recites the initial prompt back at you, or falls into
    /// repeating one phrase for as long as the silence lasts. Neither is fixable by asking; the
    /// fix is to not hand it silence in the first place.
    ///
    /// The threshold is relative to **the room**, not to the loudest moment — and getting that
    /// backwards ate the end of every sentence.
    ///
    /// It used to keep anything above a quarter of the peak. A quarter of the amplitude is about
    /// twelve decibels down, and ordinary speech has thirty decibels of range in it: a sentence
    /// falls away as it ends, which is a thing people do and not a thing they can stop doing. So
    /// the last few words sat under the line, `lastIndex` stopped in front of them, and they were
    /// cut before the model ever saw them. The symptom was not a wrong transcription — it was a
    /// correct transcription of slightly less than you said, which is far harder to notice.
    ///
    /// What has to be excluded is room tone, so room tone is what the threshold is built from:
    /// the quietest tenth of the clip, times a small factor. Trailing syllables are quiet
    /// compared to a shout and loud compared to an empty room, and that second comparison is the
    /// one that separates them from nothing.
    static func trimSilence(_ samples: Data, rate: Double, overRoom: Float = 2.5) -> Data {
        let count = samples.count / 2
        guard count > Int(rate * 0.2) else { return samples }
        let window = max(1, Int(rate * 0.05))          // 50 ms
        var energies: [Float] = []
        samples.withUnsafeBytes { (raw: UnsafeRawBufferPointer) in
            let pcm = raw.bindMemory(to: Int16.self)
            var i = 0
            while i < count {
                let end = min(i + window, count)
                var sum: Float = 0
                for j in i..<end { sum += abs(Float(pcm[j])) }
                energies.append(sum / Float(end - i))
                i = end
            }
        }
        guard let loudest = energies.max(), loudest > 0 else { return Data() }
        // The quietest tenth stands in for the room. A percentile rather than the minimum: one
        // freak-quiet window would otherwise set the level for the whole clip.
        let sorted = energies.sorted()
        let room = sorted[min(sorted.count - 1, sorted.count / 10)]
        // One level all the way through: there is no room tone here to tell speech apart from,
        // so there is nothing this can honestly cut. Whether anything was said at all is the
        // pause detector's question, and it was asked before this was called.
        guard loudest > room * overRoom else { return samples }
        // The second term matters in a recording with no room tone at all, where `room` is
        // near zero and the first term would keep every stray bit. Two per cent of the peak is
        // about thirty-four decibels down — permissive on purpose, because including a little
        // room costs nothing and cutting a word costs the word.
        let floor = max(room * overRoom, loudest * 0.02)
        guard let first = energies.firstIndex(where: { $0 > floor }),
              let last = energies.lastIndex(where: { $0 > floor }) else { return Data() }
        // A breath of room either side, so words are not clipped at their edges. 300ms rather
        // than 150: a consonant that starts a word is quieter than the vowel behind it, and the
        // padding is what stops the threshold from shaving it off.
        let pad = 6
        let from = max(0, first - pad) * window
        let to = min(count, (last + 1 + pad) * window)
        guard to > from else { return Data() }
        return samples.subdata(in: (from * 2)..<(to * 2))
    }

    /// Whether a transcript is the model stuck in a groove.
    ///
    /// "和音，和音，和音，和音，" is not a sentence anybody said; it is what happens when the
    /// decoder has nothing to go on and keeps choosing the same continuation. Cheaper to spot
    /// afterwards than to prevent, and the answer when it happens is to keep the live text.
    static func looksLikeLoop(_ text: String) -> Bool {
        let cleaned = text.replacingOccurrences(of: " ", with: "")

        // The clause check first, and with no length floor: the shortest real example measured
        // is "好,好,好。" — six characters, and unmistakably not a sentence.
        let parts = cleaned.split(whereSeparator: { "，,。.、;；！!？?".contains($0) })
            .map(String.init).filter { !$0.isEmpty }
        // Three, not four. A person saying one clause three times, punctuated, is not something
        // worth protecting from.
        if parts.count >= 3, Set(parts).count == 1 { return true }

        guard cleaned.count >= 8 else { return false }
        // Or one phrase repeated until it fills most of the output.
        for unit in 1...6 where cleaned.count >= unit * 4 {
            let chars = Array(cleaned)
            let piece = String(chars[0..<unit])
            var repeats = 0
            var i = 0
            while i + unit <= chars.count, String(chars[i..<(i + unit)]) == piece {
                repeats += 1
                i += unit
            }
            if repeats >= 4, Double(repeats * unit) / Double(chars.count) > 0.7 { return true }
        }
        // Or the same clause over and over, separated by punctuation.
        return false
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

    private static func run(_ launch: String, _ args: [String]) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: launch)
        task.arguments = args
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice
        do { try task.run() } catch { return nil }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitQuietly()
        return String(data: data, encoding: .utf8)
    }
}
