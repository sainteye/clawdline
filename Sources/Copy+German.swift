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

    let menuOpen = "Eingabeleiste öffnen"
    let menuReveal = "Zum Ziel-Tab springen"
    let menuMascot = "Maskottchen"
    let menuLogin = "Bei der Anmeldung öffnen"
    let menuEditConfig = "Konfiguration bearbeiten…"
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
