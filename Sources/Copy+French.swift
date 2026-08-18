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
    func stackConfirm(_ command: String) -> String { "Appuyez à nouveau pour exécuter :  \(command)" }
    let hintStacks = "serveurs"
    func stackTip(up: Int, total: Int) -> String { "\(up) serveurs sur \(total) actifs — ⌘S pour la liste" }
    let stackTipUnknown = "Ce projet a des serveurs ; sa commande d'état n'est pas encore approuvée — ⌘S"
    let stackUntrusted = "non approuvé"
    let stackActionStart = "démarrer"
    let stackActionRestart = "redémarrer"
    let stackActionStop = "arrêter"
    let stackActionLogs = "journal"
    let stackLogAll = "tout"
    let stackLogBack = "transcription"
    let stackActionAllow = "autoriser"
    let stackActionAgain = "à nouveau"
    func sessionTip(index: Int, total: Int) -> String { "Session \(index) sur \(total) — ⌘K pour changer" }
    let sessionWaiting = "attend ta réponse"
    let islandDone = "terminé"
    let islandAllSessions = "Toutes les sessions…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) attend ta réponse"
                          : "\(labels.count) sessions attendent ta réponse"
    }
    func statusWorking(_ count: Int) -> String { "\(count) en cours" }

    let settingsTitle = "Réglages de Clawdline"
    let settingsGeneral = "Général"
    let settingsBar = "La barre"
    let settingsReading = "Lecture"
    let settingsVoice = "Dictée"
    let settingsHotkey = "Raccourci"
    let settingsRecording = "Appuie sur des touches…"
    let settingsScope = "Actif dans"
    let settingsScopeGlobal = "Dans toutes les apps"
    let settingsScopeHint = "Identifiants de bundle, séparés par des virgules. Vide = partout."
    let settingsLanguage = "Langue"
    let settingsReopen = "Revenir avec le terminal"
    let settingsFollow = "Déplacer aussi l’onglet du terminal"
    let settingsNotch = "Habiter l’encoche"
    let settingsNotchHint = "Un personnage dans le boîtier de la caméra. Désactivé = rien n’est dessiné, aucune fenêtre créée."
    let settingsPosition = "Hauteur à l’écran"
    let settingsWidth = "Largeur"
    let settingsOpacity = "Opacité de la carte"
    let settingsImagesPaste = "Envoyer les images comme images"
    let settingsShow = "Afficher"
    let settingsPaneHeight = "Hauteur du volet"
    let settingsTextSize = "Taille du texte"
    let settingsPaneFont = "Police du volet"
    let settingsBlur = "Flou derrière"
    let settingsNewestFirst = "Plus récent en haut"
    let settingsEngine = "Reconnaissance"
    let settingsSettle = "Une pause termine la phrase"
    let settingsStop = "Un silence termine la séance"
    let settingsAuto = "Auto"
    let settingsTranscript = "Transcription"
    let settingsTerminal = "Terminal"
    let settingsOff = "Désactivé"
    let settingsHooks = "Hooks Claude Code"
    let settingsHooksHint = "Une fois installés, Claude Code signale l'instant où un tour commence, se termine ou attend une réponse, au lieu que Clawdline l'apprenne au prochain coup d'œil. Tout se lit toujours sur l'écran ; ceci ne décide que du moment de la lecture."
    let settingsHooksInstall = "Installer"
    let settingsHooksRemove = "Retirer"
    let settingsHooksOff = "Non installés — tout est lu sur l'écran"
    let settingsHooksOn = "Installés — aucune session ne s'est encore signalée"
    let settingsHooksLive = "Installés, et les sessions se signalent"
    let settingsRemote = "À distance"
    let settingsRemoteServe = "Répondre en HTTP"
    let settingsRemoteHint = "Publie la liste des sessions sur 127.0.0.1, pour qu'un navigateur, un téléphone au bout d'un tunnel ou un script puisse la lire. Désactivé tant que vous ne l'activez pas : une socket à l'écoute livre les noms des dépôts, les branches et les titres des tâches."
    let settingsRemoteDevices = "Appareils jumelés"
    let settingsRemoteNoDevices = "Aucun pour l'instant — rien en dehors de ce Mac ne peut rien lire"
    let settingsRemoteRevokeAll = "Tout déconnecter"
    let settingsRemoteOpen = "Ouvrir dans un navigateur"
    let pairingIgnore = "Ignorer"
    func pairingAsks(_ device: String) -> String { "\(device) veut se jumeler à ce Mac" }
    func pairingCode(_ code: String) -> String {
        """
        Saisissez ce code sur cet appareil :

        \(code)

        Il est valable deux minutes. Si vous ne venez pas de le demander, ignorez-le — sans ce \
        code, celui qui a demandé ne peut pas aller au bout.
        """
    }
    let settingsTunnel = "Joignable de l'extérieur"
    let settingsTunnelQuick = "Une adresse générée"
    let settingsTunnelNamed = "Mon propre domaine"
    let settingsTunnelHostname = "Nom d'hôte"
    let settingsTunnelHint = "Ouvre une connexion sortante via cloudflared — aucune redirection de port, rien à l'écoute sur votre réseau. Ne démarre pas tant qu'aucun appareil n'est jumelé, parce que derrière le tunnel il y a le nom de chaque dépôt et le titre de chaque tâche de ce Mac."
    let settingsRemoteWrite = "Laisser les appareils jumelés écrire"
    let settingsRemoteWriteHint = "Désactivé, un appareil jumelé ne peut que lire. Activé, il peut envoyer du texte dans une session et en ouvrir de nouvelles — ce qui exécute du code sur ce Mac, puisque c'est ce que fait Claude Code. Ce n'est pas la même décision que celle du dessus, donc ce n'est pas le même interrupteur."
    let settingsRemotePhone = "Jumeler un téléphone…"
    let settingsRemotePhoneHint = "Affiche un code à scanner. Il porte une clé bien à lui : une photo de ce code, c'est un appareil que vous voyez dans cette liste et que vous pouvez retirer — pas la clé de ce Mac."
    let pairingScanTitle = "Scannez ceci avec le téléphone"
    let pushWaiting = "attend une réponse"
    let settingsOpenFile = "Ouvrir le fichier de configuration…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f s", value) }

    let menuOpen = "Ouvrir la barre"
    let menuReveal = "Aller à l'onglet cible"
    let menuMascot = "Mascotte"
    let menuLogin = "Ouvrir à la connexion"
    let menuEditConfig = "Réglages…"
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
