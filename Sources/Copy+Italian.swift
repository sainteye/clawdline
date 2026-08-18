import Foundation

struct Italian: Copy {
    let placeholder = "Scrivi a Claude Code…"

    let hintSend = "invia"
    let hintNewline = "a capo"
    let hintSwitch = "cambia"
    let hintList = "elenco"
    let hintMascot = "mascotte"
    let hintOutput = "output"
    let hintFullscreen = "schermo intero"
    let hintKeys = "tasti"
    let hintTextSize = "dimensione"
    let hintOrder = "inverti"
    let hintVoice = "detta"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "In ascolto su questo Mac — premi di nuovo per fermare"
                 : "In ascolto — questa lingua la trascrive Apple, non questo Mac"
    }
    let voiceNoPermission = "La dettatura ha bisogno di microfono e riconoscimento vocale"
    let voiceUnavailable = "La dettatura non è disponibile in questo momento"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper sta rileggendo… %.1f s", seconds)
    }
    let whisperMissing = "Whisper non è installato — vedi docs/whisper.md"
    let whisperNothingHeard = "Non ho sentito nulla"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Dettatura: Apple, poi Whisper (\(model))"
        case .noBinary: return "Dettatura: solo Apple — manca whisper-cli"
        case .noModel: return "Dettatura: solo Apple — whisper-cli c'è, manca il modello"
        }
    }
    func voiceListeningWhisper() -> String {
        "In ascolto — quando ti fermi Whisper dà un'altra letta"
    }

    let scanning = "Ricerca…"
    let noSession = "Nessuna sessione di Claude Code trovata"
    let nothingToSend = "Nessuna destinazione — avvia prima Claude Code in un terminale"
    let sendFailed = "Invio non riuscito"
    let itermSilent = "iTerm2 non ha risposto"
    let scriptMissing = "Manca iterm.js — bundle dell'app danneggiato?"
    let cannotList = "Non è stato possibile leggere le sessioni di iTerm2"
    let noOutput = "In questa sessione non c'è ancora nulla da leggere."
    func outputSize(_ pt: Int) -> String { "Testo dell'output \(pt) pt — ⌘J per vederlo" }
    func foldedTools(_ count: Int) -> String { "\(count) passaggi" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Prima i più recenti" : "Prima i più vecchi"
    }
    func backlogNow(_ count: Int) -> String { "ora \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Percorso aggiunto — Claude Code lo legge" : "Aggiunti \(count) percorsi"
    }
    func stackConfirm(_ command: String) -> String { "Premi di nuovo per eseguire:  \(command)" }
    let hintStacks = "server"
    func stackTip(up: Int, total: Int) -> String { "\(up) di \(total) server attivi — ⌘S per l\'elenco" }
    let stackTipUnknown = "Questo progetto ha server; il suo comando di stato non è ancora attendibile — ⌘S"
    let stackUntrusted = "non approvato"
    let stackActionStart = "avvia"
    let stackActionRestart = "riavvia"
    let stackActionStop = "ferma"
    let stackActionLogs = "log"
    let stackLogAll = "tutti"
    let stackLogBack = "trascrizione"
    let stackActionAllow = "consenti"
    let stackActionAgain = "di nuovo"
    func sessionTip(index: Int, total: Int) -> String { "Sessione \(index) di \(total) — ⌘K per cambiare" }
    let sessionWaiting = "aspetta te"
    let islandDone = "fatto"
    let islandAllSessions = "Tutte le sessioni…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) sta aspettando te"
                          : "\(labels.count) sessioni stanno aspettando te"
    }
    func statusWorking(_ count: Int) -> String { "\(count) in corso" }

    let settingsTitle = "Impostazioni di Clawdline"
    let settingsGeneral = "Generale"
    let settingsBar = "La barra"
    let settingsReading = "Lettura"
    let settingsVoice = "Dettatura"
    let settingsHotkey = "Scorciatoia"
    let settingsRecording = "Premi dei tasti…"
    let settingsScope = "Attiva in"
    let settingsScopeGlobal = "In ogni app"
    let settingsScopeHint = "Bundle id separati da virgole. Vuoto significa ovunque."
    let settingsLanguage = "Lingua"
    let settingsReopen = "Torna con il terminale"
    let settingsFollow = "Sposta anche la scheda del terminale"
    let settingsNotch = "Vivi nel notch"
    let settingsNotchHint = "Un personaggio nell’alloggiamento della fotocamera. Off è off: non si disegna nulla e non si crea alcuna finestra."
    let settingsPosition = "Altezza sullo schermo"
    let settingsWidth = "Larghezza"
    let settingsOpacity = "Opacità della scheda"
    let settingsImagesPaste = "Invia le immagini come immagini"
    let settingsShow = "Mostra"
    let settingsPaneHeight = "Altezza del riquadro"
    let settingsTextSize = "Dimensione del testo"
    let settingsPaneFont = "Font del riquadro"
    let settingsBlur = "Sfocatura dietro"
    let settingsNewestFirst = "Più recenti in alto"
    let settingsEngine = "Riconoscimento"
    let settingsSettle = "Una pausa chiude la frase"
    let settingsStop = "Un silenzio chiude la sessione"
    let settingsAuto = "Auto"
    let settingsTranscript = "Trascrizione"
    let settingsTerminal = "Terminale"
    let settingsOff = "Off"
    let settingsHooks = "Hook di Claude Code"
    let settingsHooksHint = "Una volta installati, Claude Code segnala l'istante in cui un turno inizia, finisce o aspetta una risposta, invece che Clawdline se ne accorga al controllo successivo. Tutto si legge sempre dallo schermo; questo decide solo quanto in fretta."
    let settingsHooksInstall = "Installa"
    let settingsHooksRemove = "Rimuovi"
    let settingsHooksOff = "Non installati — tutto viene letto dallo schermo"
    let settingsHooksOn = "Installati — nessuna sessione si è ancora fatta sentire"
    let settingsHooksLive = "Installati, e le sessioni si fanno sentire"
    let settingsRemote = "Accesso remoto"
    let settingsRemoteServe = "Rispondi via HTTP"
    let settingsRemoteHint = "Pubblica l'elenco delle sessioni su 127.0.0.1, così possono leggerlo un browser, un telefono in fondo a un tunnel o uno script. Resta spento finché non lo accendi: un socket in ascolto consegna i nomi dei repository, i branch e i titoli delle attività."
    let settingsRemoteDevices = "Dispositivi abbinati"
    let settingsRemoteNoDevices = "Nessuno per ora — fuori da questo Mac nessuno può leggere niente"
    let settingsRemoteRevokeAll = "Disconnetti tutto"
    let settingsRemoteOpen = "Apri nel browser"
    let pairingIgnore = "Ignora"
    func pairingAsks(_ device: String) -> String { "\(device) vuole abbinarsi a questo Mac" }
    func pairingCode(_ code: String) -> String {
        """
        Digita questo codice su quel dispositivo:

        \(code)

        Vale due minuti. Se non l'hai appena chiesto tu, ignoralo — senza questo codice chi \
        ha chiesto non può arrivare in fondo.
        """
    }
    let settingsTunnel = "Raggiungibile da fuori"
    let settingsTunnelQuick = "Un indirizzo generato"
    let settingsTunnelNamed = "Il mio dominio"
    let settingsTunnelHostname = "Nome host"
    let settingsTunnelHint = "Apre una connessione in uscita attraverso cloudflared — nessun port forwarding, niente in ascolto sulla tua rete. Non parte finché non c'è un dispositivo abbinato, perché dietro al tunnel c'è il nome di ogni repository e il titolo di ogni attività su questo Mac."
    let settingsRemoteWrite = "Lascia scrivere ai dispositivi abbinati"
    let settingsRemoteWriteHint = "Spento, un dispositivo abbinato può solo leggere. Acceso, può mandare testo dentro una sessione e avviarne di nuove — e questo esegue codice su questo Mac, perché è quello che fa Claude Code. È una decisione diversa da quella sopra, quindi è un interruttore a parte."
    let settingsRemotePhone = "Abbina un telefono…"
    let settingsRemotePhoneHint = "Mostra un codice da inquadrare. Porta con sé una chiave tutta sua: una fotografia di quel codice è un dispositivo che vedi in questo elenco e che puoi togliere di nuovo — non la chiave di questo Mac."
    let pairingScanTitle = "Inquadra questo con il telefono"
    let pushWaiting = "aspetta una risposta"
    let settingsOpenFile = "Apri il file di configurazione…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f s", value) }

    let menuOpen = "Apri la barra"
    let menuReveal = "Vai alla scheda di destinazione"
    let menuMascot = "Mascotte"
    let menuLogin = "Apri all'accesso"
    let menuEditConfig = "Impostazioni…"
    let menuReload = "Ricarica configurazione"
    let menuQuit = "Esci da Clawdline"
    let menuNoTarget = "(non ancora rilevato)"

    func hotkeyFailedTitle(_ combo: String) -> String { "Non è stato possibile registrare \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Con ogni probabilità la usa già un'altra app — Spotlight, un commutatore di \
        tastiera, BetterTouchTool e simili.

        Scegline un'altra: modifica "hotkey" in \(configPath), poi scegli \
        "Ricarica configurazione" dalla barra dei menu.

        Fino ad allora, il ✳ nella barra dei menu apre comunque la barra.
        """
    }
    let loginFailed = "Non è stato possibile impostare l'apertura all'accesso"
}
