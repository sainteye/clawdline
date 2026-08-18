import Foundation

struct German: Copy {
    let placeholder = "Nachricht an Claude Code…"

    let hintSend = "senden"
    let hintNewline = "neue Zeile"
    let hintSwitch = "wechseln"
    let hintList = "Liste"
    let hintMascot = "Maskottchen"
    let hintOutput = "Ausgabe"
    let hintFullscreen = "Vollbild"
    let hintKeys = "Tasten"
    let hintTextSize = "Schriftgröße"
    let hintOrder = "umkehren"
    let hintVoice = "diktieren"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Hört auf diesem Mac zu — nochmal drücken zum Beenden"
                 : "Hört zu — diese Sprache transkribiert Apple, nicht dieser Mac"
    }
    let voiceNoPermission = "Das Diktat braucht Zugriff auf Mikrofon und Spracherkennung"
    let voiceUnavailable = "Das Diktat ist gerade nicht verfügbar"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper liest noch einmal… %.1f s", seconds)
    }
    let whisperMissing = "Whisper ist nicht installiert — siehe docs/whisper.md"
    let whisperNothingHeard = "Nichts gehört"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Diktat: Apple, danach Whisper (\(model))"
        case .noBinary: return "Diktat: nur Apple — kein whisper-cli"
        case .noModel: return "Diktat: nur Apple — whisper-cli ist da, das Modell fehlt"
        }
    }
    func voiceListeningWhisper() -> String {
        "Hört zu — Whisper schaut noch einmal drüber, wenn du aufhörst"
    }

    let scanning = "Suche…"
    let noSession = "Keine Claude-Code-Sitzung gefunden"
    let nothingToSend = "Kein Ziel — starte Claude Code zuerst in einem Terminal"
    let sendFailed = "Konnte nicht senden"
    let itermSilent = "iTerm2 hat nicht geantwortet"
    let scriptMissing = "iterm.js fehlt — beschädigtes App-Bundle?"
    let cannotList = "Konnte die iTerm2-Sitzungen nicht lesen"
    let noOutput = "In dieser Sitzung gibt es noch nichts zu lesen."
    func outputSize(_ pt: Int) -> String { "Ausgabetext \(pt) pt — ⌘J zum Anzeigen" }
    func foldedTools(_ count: Int) -> String { "\(count) Schritte" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Neueste zuerst" : "Älteste zuerst"
    }
    func backlogNow(_ count: Int) -> String { "jetzt \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Pfad eingefügt — Claude Code liest ihn" : "\(count) Pfade eingefügt"
    }
    func stackConfirm(_ command: String) -> String { "Erneut drücken zum Ausführen:  \(command)" }
    let hintStacks = "Server"
    func stackTip(up: Int, total: Int) -> String { "\(up) von \(total) Servern laufen — ⌘S für die Liste" }
    let stackTipUnknown = "Dieses Projekt hat Server; sein Status-Befehl ist noch nicht freigegeben — ⌘S"
    let stackUntrusted = "nicht freigegeben"
    let stackActionStart = "starten"
    let stackActionRestart = "neu starten"
    let stackActionStop = "stoppen"
    let stackActionLogs = "Protokoll"
    let stackLogAll = "alle"
    let stackLogBack = "Transkript"
    let stackActionAllow = "zulassen"
    let stackActionAgain = "nochmal"
    func sessionTip(index: Int, total: Int) -> String { "Sitzung \(index) von \(total) — ⌘K zum Wechseln" }
    let sessionWaiting = "wartet auf dich"
    let islandDone = "fertig"
    let islandAllSessions = "Alle Sitzungen …"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) wartet auf dich"
                          : "\(labels.count) Sitzungen warten auf dich"
    }
    func statusWorking(_ count: Int) -> String { "\(count) laufen" }

    let settingsTitle = "Clawdline Einstellungen"
    let settingsGeneral = "Allgemein"
    let settingsBar = "Die Leiste"
    let settingsReading = "Lesen"
    let settingsVoice = "Diktat"
    let settingsHotkey = "Tastenkürzel"
    let settingsRecording = "Tasten drücken …"
    let settingsScope = "Aktiv in"
    let settingsScopeGlobal = "In jeder App"
    let settingsScopeHint = "Bundle-IDs, durch Komma getrennt. Leer heißt überall."
    let settingsLanguage = "Sprache"
    let settingsReopen = "Mit dem Terminal zurückkommen"
    let settingsFollow = "Den Tab im Terminal mitziehen"
    let settingsNotch = "In der Notch wohnen"
    let settingsNotchHint = "Eine Figur im Kameragehäuse. Aus ist aus — nichts wird gezeichnet, kein Fenster angelegt."
    let settingsPosition = "Höhe am Bildschirm"
    let settingsWidth = "Breite"
    let settingsOpacity = "Deckkraft der Karte"
    let settingsImagesPaste = "Bilder als Bilder senden"
    let settingsShow = "Zeigen"
    let settingsPaneHeight = "Höhe des Bereichs"
    let settingsTextSize = "Textgröße"
    let settingsPaneFont = "Schrift des Bereichs"
    let settingsBlur = "Unschärfe dahinter"
    let settingsNewestFirst = "Neueste zuerst"
    let settingsEngine = "Erkennung"
    let settingsSettle = "Eine Pause beendet den Satz"
    let settingsStop = "Stille beendet die Sitzung"
    let settingsAuto = "Automatisch"
    let settingsTranscript = "Verlauf"
    let settingsTerminal = "Terminal"
    let settingsOff = "Aus"
    let settingsHooks = "Claude-Code-Hooks"
    let settingsHooksHint = "Sind sie eingerichtet, meldet Claude Code den Moment, in dem ein Durchgang beginnt, endet oder eine Antwort braucht — statt dass Clawdline es beim nächsten Blick bemerkt. Gelesen wird weiterhin vom Bildschirm; das hier bestimmt nur, wie schnell."
    let settingsHooksInstall = "Einrichten"
    let settingsHooksRemove = "Entfernen"
    let settingsHooksOff = "Nicht eingerichtet — alles kommt vom Bildschirm"
    let settingsHooksOn = "Eingerichtet — noch keine Sitzung hat sich gemeldet"
    let settingsHooksLive = "Eingerichtet, Sitzungen melden sich"
    let settingsOpenFile = "Konfigurationsdatei öffnen …"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f s", value) }

    let menuOpen = "Eingabeleiste öffnen"
    let menuReveal = "Zum Ziel-Tab springen"
    let menuMascot = "Maskottchen"
    let menuLogin = "Bei der Anmeldung öffnen"
    let menuEditConfig = "Einstellungen …"
    let menuReload = "Konfiguration neu laden"
    let menuQuit = "Clawdline beenden"
    let menuNoTarget = "(noch nicht erkannt)"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) ließ sich nicht registrieren" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Wahrscheinlich benutzt eine andere App das Kürzel schon — Spotlight, ein \
        Eingabequellen-Umschalter, BetterTouchTool und dergleichen.

        Nimm ein anderes: Bearbeite „hotkey“ in \(configPath) und wähle dann in der \
        Menüleiste „Konfiguration neu laden“.

        Bis dahin öffnet das ✳ in der Menüleiste die Eingabeleiste weiterhin.
        """
    }
    let loginFailed = "Konnte den Start bei der Anmeldung nicht einrichten"
}
