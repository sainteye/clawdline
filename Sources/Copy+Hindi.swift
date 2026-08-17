import Foundation

/// India has the largest population of open-source contributors on GitHub, and almost all of
/// them work in English — so this is here for the people who would rather not, which is a real
/// group and not the same one. Technical nouns stay in Latin script (`terminal`, `session`,
/// `Whisper`) because that is how they are said, not because they were left untranslated.
struct Hindi: Copy {
    let placeholder = "Claude Code को लिखें…"

    let hintSend = "भेजें"
    let hintNewline = "नई पंक्ति"
    let hintSwitch = "बदलें"
    let hintList = "सूची"
    let hintMascot = "मैस्कॉट"
    let hintOutput = "आउटपुट"
    let hintFullscreen = "पूरी स्क्रीन"
    let hintKeys = "कुंजियाँ"
    let hintTextSize = "अक्षर आकार"
    let hintOrder = "उल्टा क्रम"
    let hintVoice = "बोलकर लिखें"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "इसी Mac पर सुन रहा है — रोकने के लिए फिर दबाएँ"
                 : "सुन रहा है — यह भाषा Apple के यहाँ लिखी जाती है, इस Mac पर नहीं"
    }
    let voiceNoPermission = "बोलकर लिखने के लिए माइक्रोफ़ोन और वाक् पहचान की अनुमति चाहिए"
    let voiceUnavailable = "अभी बोलकर लिखना उपलब्ध नहीं है"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper दोबारा पढ़ रहा है… %.1f से॰", seconds)
    }
    let whisperMissing = "Whisper इंस्टॉल नहीं है — docs/whisper.md देखें"
    let whisperNothingHeard = "कुछ सुनाई नहीं दिया"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "बोलकर लिखना: पहले Apple, फिर Whisper (\(model))"
        case .noBinary: return "बोलकर लिखना: सिर्फ़ Apple — whisper-cli नहीं है"
        case .noModel: return "बोलकर लिखना: सिर्फ़ Apple — whisper-cli है, मॉडल नहीं"
        }
    }
    func voiceListeningWhisper() -> String {
        "सुन रहा है — आपके रुकते ही Whisper एक बार और पढ़ेगा"
    }

    let scanning = "खोज रहा है…"
    let noSession = "Claude Code का कोई session नहीं मिला"
    let nothingToSend = "भेजने की कोई जगह नहीं — पहले किसी terminal में Claude Code चलाएँ"
    let sendFailed = "भेजा नहीं जा सका"
    let itermSilent = "iTerm2 ने जवाब नहीं दिया"
    let scriptMissing = "iterm.js नहीं मिला — app bundle खराब है?"
    let cannotList = "iTerm2 के sessions पढ़े नहीं जा सके"
    let noOutput = "इस session में अभी पढ़ने को कुछ नहीं है।"
    func outputSize(_ pt: Int) -> String { "आउटपुट का अक्षर आकार \(pt)pt — देखने के लिए ⌘J" }
    func foldedTools(_ count: Int) -> String { "\(count) चरण" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "नए पहले" : "पुराने पहले"
    }
    func backlogNow(_ count: Int) -> String { "अभी \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "पथ जोड़ दिया — Claude Code उसे पढ़ लेगा" : "\(count) पथ जोड़े"
    }

    let menuOpen = "इनपुट बार खोलें"
    let menuReveal = "लक्ष्य tab पर जाएँ"
    let menuMascot = "मैस्कॉट"
    let menuLogin = "लॉगिन पर चालू करें"
    let menuEditConfig = "सेटिंग्स संपादित करें…"
    let menuReload = "सेटिंग्स फिर से पढ़ें"
    let menuQuit = "Clawdline बंद करें"
    let menuNoTarget = "(अभी पता नहीं चला)"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) दर्ज नहीं हो सका" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        सबसे संभव है कि कोई और app इसे पहले से ले चुका है — Spotlight, कीबोर्ड बदलने वाला \
        टूल, BetterTouchTool वग़ैरह।

        दूसरा चुनें: \(configPath) में "hotkey" बदलें, फिर मेन्यू बार से \
        "सेटिंग्स फिर से पढ़ें" चुनें।

        तब तक मेन्यू बार का ✳ इनपुट बार खोलता रहेगा।
        """
    }
    let loginFailed = "लॉगिन पर चालू करना सेट नहीं हो सका"
}
