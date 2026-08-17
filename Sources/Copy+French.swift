import Foundation

struct French: Copy {
    let placeholder = "Écrire à Claude Code…"

    let hintSend = "envoyer"
    let hintNewline = "nouvelle ligne"
    let hintSwitch = "changer"
    let hintList = "liste"
    let hintMascot = "mascotte"
    let hintOutput = "sortie"
    let hintFullscreen = "plein écran"
    let hintKeys = "touches"
    let hintTextSize = "taille"
    let hintOrder = "inverser"
    let hintVoice = "dicter"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "À l'écoute sur ce Mac — appuyez à nouveau pour arrêter"
                 : "À l'écoute — cette langue est transcrite par Apple, pas sur ce Mac"
    }
    let voiceNoPermission = "La dictée a besoin du micro et de la reconnaissance vocale"
    let voiceUnavailable = "La dictée n'est pas disponible pour le moment"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper relit… %.1f s", seconds)
    }
    let whisperMissing = "Whisper n'est pas installé — voir docs/whisper.md"
    let whisperNothingHeard = "Rien entendu"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Dictée : Apple, puis Whisper (\(model))"
        case .noBinary: return "Dictée : Apple seulement — pas de whisper-cli"
        case .noModel: return "Dictée : Apple seulement — whisper-cli est là, pas de modèle"
        }
    }
    func voiceListeningWhisper() -> String {
        "À l'écoute — Whisper relit tout quand vous vous arrêtez"
    }

    let scanning = "Recherche…"
    let noSession = "Aucune session Claude Code trouvée"
    let nothingToSend = "Aucune destination — lancez d'abord Claude Code dans un terminal"
    let sendFailed = "Envoi impossible"
    let itermSilent = "iTerm2 n'a pas répondu"
    let scriptMissing = "iterm.js est introuvable — paquet de l'app abîmé ?"
    let cannotList = "Impossible de lire les sessions iTerm2"
    let noOutput = "Rien à lire dans cette session pour l'instant."
    func outputSize(_ pt: Int) -> String { "Texte de sortie \(pt) pt — ⌘J pour l'afficher" }
    func foldedTools(_ count: Int) -> String { "\(count) étapes" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Le plus récent en haut" : "Le plus ancien en haut"
    }
    func backlogNow(_ count: Int) -> String { "maintenant \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Chemin ajouté — Claude Code le lit" : "\(count) chemins ajoutés"
    }

    let menuOpen = "Ouvrir la barre"
    let menuReveal = "Aller à l'onglet cible"
    let menuMascot = "Mascotte"
    let menuLogin = "Ouvrir à la connexion"
    let menuEditConfig = "Modifier la configuration…"
    let menuReload = "Recharger la configuration"
    let menuQuit = "Quitter Clawdline"
    let menuNoTarget = "(pas encore détecté)"

    func hotkeyFailedTitle(_ combo: String) -> String { "Impossible d'enregistrer \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Une autre app l'utilise sans doute déjà — Spotlight, un changeur de clavier, \
        BetterTouchTool, etc.

        Choisissez-en un autre : modifiez « hotkey » dans \(configPath), puis choisissez \
        « Recharger la configuration » dans la barre des menus.

        En attendant, le ✳ de la barre des menus ouvre toujours la barre.
        """
    }
    let loginFailed = "Impossible de configurer l'ouverture à la connexion"
}
