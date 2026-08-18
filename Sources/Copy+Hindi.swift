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
    func stackConfirm(_ command: String) -> String { "चलाने के लिए फिर से दबाएँ:  \(command)" }
    let hintStacks = "सर्वर"
    func stackTip(up: Int, total: Int) -> String { "\(total) में से \(up) सर्वर चालू — सूची के लिए ⌘S" }
    let stackTipUnknown = "इस प्रोजेक्ट में सर्वर हैं, पर इसकी status कमांड पर अभी भरोसा नहीं किया गया — ⌘S"
    let stackUntrusted = "अभी अनुमति नहीं"
    let stackActionStart = "शुरू"
    let stackActionRestart = "रीस्टार्ट"
    let stackActionStop = "रोकें"
    let stackActionLogs = "लॉग"
    let stackLogAll = "सभी"
    let stackLogBack = "ट्रांसक्रिप्ट"
    let stackActionAllow = "अनुमति"
    let stackActionAgain = "फिर दबाएँ"
    func sessionTip(index: Int, total: Int) -> String { "\(total) में से \(index) वाँ सेशन — बदलने के लिए ⌘K" }
    let sessionWaiting = "आपके जवाब का इंतज़ार"
    let islandDone = "हो गया"
    let islandAllSessions = "सभी सेशन…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) आपके जवाब का इंतज़ार कर रहा है"
                          : "\(labels.count) सेशन आपके जवाब का इंतज़ार कर रहे हैं"
    }
    func statusWorking(_ count: Int) -> String { "\(count) चल रहे हैं" }

    let settingsTitle = "Clawdline सेटिंग्ज़"
    let settingsGeneral = "सामान्य"
    let settingsBar = "बार"
    let settingsReading = "पढ़ना"
    let settingsVoice = "श्रुतलेख"
    let settingsHotkey = "हॉटकी"
    let settingsRecording = "कुंजियाँ दबाएँ…"
    let settingsScope = "कहाँ चले"
    let settingsScopeGlobal = "हर ऐप में"
    let settingsScopeHint = "बंडल आईडी, अल्पविराम से अलग। खाली मतलब हर जगह।"
    let settingsLanguage = "भाषा"
    let settingsReopen = "टर्मिनल के साथ लौटें"
    let settingsFollow = "टर्मिनल का टैब भी बदलें"
    let settingsNotch = "नॉच में रहे"
    let settingsNotchHint = "कैमरा हाउसिंग में एक किरदार। बंद माने बंद — कुछ नहीं बनता, कोई विंडो नहीं।"
    let settingsPosition = "स्क्रीन पर ऊँचाई"
    let settingsWidth = "चौड़ाई"
    let settingsOpacity = "कार्ड की अपारदर्शिता"
    let settingsImagesPaste = "तस्वीरें तस्वीरों की तरह भेजें"
    let settingsShow = "दिखाएँ"
    let settingsPaneHeight = "पैनल की ऊँचाई"
    let settingsTextSize = "टेक्स्ट का आकार"
    let settingsPaneFont = "पैनल का फ़ॉन्ट"
    let settingsBlur = "पीछे धुँधलापन"
    let settingsNewestFirst = "नया पहले"
    let settingsEngine = "पहचानकर्ता"
    let settingsSettle = "ठहराव वाक्य ख़त्म करता है"
    let settingsStop = "सन्नाटा सत्र ख़त्म करता है"
    let settingsAuto = "स्वतः"
    let settingsTranscript = "ट्रांसक्रिप्ट"
    let settingsTerminal = "टर्मिनल"
    let settingsOff = "बंद"
    let settingsHooks = "Claude Code हुक"
    let settingsHooksHint = "ये लगे होने पर Claude Code उसी क्षण बता देता है जब कोई बारी शुरू होती है, ख़त्म होती है या जवाब माँगती है — Clawdline को अगली बार देखने तक इंतज़ार नहीं करना पड़ता। पढ़ा फिर भी स्क्रीन से ही जाता है; यह सिर्फ़ यह तय करता है कि कितनी जल्दी।"
    let settingsHooksInstall = "लगाएँ"
    let settingsHooksRemove = "हटाएँ"
    let settingsHooksOff = "नहीं लगे — सब कुछ स्क्रीन से पढ़ा जाता है"
    let settingsHooksOn = "लगे हैं — अभी किसी सत्र ने कुछ नहीं बताया"
    let settingsHooksLive = "लगे हैं, और सत्र बता रहे हैं"
    let settingsRemote = "रिमोट पहुँच"
    let settingsRemoteServe = "HTTP पर जवाब दें"
    let settingsRemoteHint = "session की सूची 127.0.0.1 पर देता है, ताकि कोई browser, tunnel के पार रखा फ़ोन, या कोई script उसे पढ़ सके। जब तक आप ख़ुद चालू न करें, बंद रहता है: सुनता हुआ एक socket repository के नाम, branch और काम के शीर्षक सौंप देता है।"
    let settingsRemoteDevices = "जुड़े हुए डिवाइस"
    let settingsRemoteNoDevices = "अभी कोई नहीं — इस Mac के बाहर से कुछ भी नहीं पढ़ा जा सकता"
    let settingsRemoteRevokeAll = "सभी हटाएँ"
    let settingsRemoteOpen = "browser में खोलें"
    let pairingIgnore = "अनदेखा करें"
    func pairingAsks(_ device: String) -> String { "\(device) इस Mac से जुड़ना चाहता है" }
    func pairingCode(_ code: String) -> String {
        """
        उस डिवाइस में यह कोड डालें:

        \(code)

        यह दो मिनट तक चलता है। अगर अभी आपने यह नहीं माँगा, तो अनदेखा कर दें — जिसने माँगा \
        है, वह इस कोड के बिना आगे नहीं बढ़ सकता।
        """
    }
    let settingsTunnel = "बाहर से पहुँच"
    let settingsTunnelQuick = "अपने आप बना पता"
    let settingsTunnelNamed = "मेरा अपना डोमेन"
    let settingsTunnelHostname = "होस्टनेम"
    let settingsTunnelHint = "cloudflared के ज़रिये इसी Mac से बाहर की ओर जुड़ता है — न port forwarding, न आपके नेटवर्क पर कुछ सुनता हुआ। जब तक कोई डिवाइस जुड़ा न हो, यह चालू नहीं होगा, क्योंकि tunnel के उस पार इस Mac का हर repository नाम और हर काम का शीर्षक है।"
    let settingsRemoteWrite = "जुड़े डिवाइस को लिखने दें"
    let settingsRemoteWriteHint = "बंद हो तो जुड़ा हुआ डिवाइस सिर्फ़ पढ़ सकता है। चालू हो तो वह किसी session में लिख भी सकता है और नए session शुरू भी कर सकता है — यानी इस Mac पर कोड चलता है, क्योंकि Claude Code यही करता है। यह ऊपर वाले से अलग फ़ैसला है, इसलिए इसका स्विच भी अलग है।"
    let settingsRemotePhone = "फ़ोन जोड़ें…"
    let settingsRemotePhoneHint = "scan करने के लिए एक code दिखाता है। उस code की अपनी एक चाबी होती है, इसलिए उसकी तस्वीर से बनता है बस एक डिवाइस — जो इसी सूची में दिखेगा और जिसे कभी भी हटाया जा सकता है — इस Mac की अपनी चाबी नहीं।"
    let pairingScanTitle = "इसे फ़ोन से scan करें"
    let pushWaiting = "जवाब का इंतज़ार कर रहा है"
    let settingsOpenFile = "कॉन्फ़िग फ़ाइल खोलें…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f से॰", value) }

    let menuOpen = "इनपुट बार खोलें"
    let menuReveal = "लक्ष्य tab पर जाएँ"
    let menuMascot = "मैस्कॉट"
    let menuLogin = "लॉगिन पर चालू करें"
    let menuEditConfig = "सेटिंग्ज़…"
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
