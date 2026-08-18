import Foundation

/// One entry for both sides of the Atlantic. The interface words here do not differ enough to
/// justify two translations that would then drift apart; where they nearly do — tela / ecrã —
/// the Brazilian form wins, because that is where most of the readers are.
struct Portuguese: Copy {
    let placeholder = "Escreva para o Claude Code…"

    let hintSend = "enviar"
    let hintNewline = "nova linha"
    let hintSwitch = "trocar"
    let hintList = "lista"
    let hintMascot = "mascote"
    let hintOutput = "saída"
    let hintFullscreen = "tela cheia"
    let hintKeys = "teclas"
    let hintTextSize = "tamanho"
    let hintOrder = "inverter"
    let hintVoice = "ditar"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Ouvindo neste Mac — pressione de novo para parar"
                 : "Ouvindo — este idioma é transcrito pela Apple, não neste Mac"
    }
    let voiceNoPermission = "O ditado precisa de acesso ao microfone e ao reconhecimento de fala"
    let voiceUnavailable = "O ditado não está disponível agora"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "O Whisper está relendo… %.1f s", seconds)
    }
    let whisperMissing = "O Whisper não está instalado — veja docs/whisper.md"
    let whisperNothingHeard = "Não ouvi nada"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Ditado: Apple e depois Whisper (\(model))"
        case .noBinary: return "Ditado: só Apple — falta o whisper-cli"
        case .noModel: return "Ditado: só Apple — o whisper-cli está aí, falta o modelo"
        }
    }
    func voiceListeningWhisper() -> String {
        "Ouvindo — o Whisper dá outra olhada quando você parar"
    }

    let scanning = "Procurando…"
    let noSession = "Nenhuma sessão do Claude Code encontrada"
    let nothingToSend = "Não há para onde enviar — abra o Claude Code num terminal primeiro"
    let sendFailed = "Não foi possível enviar"
    let itermSilent = "O iTerm2 não respondeu"
    let scriptMissing = "Falta o iterm.js — pacote do app corrompido?"
    let cannotList = "Não foi possível ler as sessões do iTerm2"
    let noOutput = "Ainda não há nada para ler nesta sessão."
    func outputSize(_ pt: Int) -> String { "Texto da saída \(pt) pt — ⌘J para ver" }
    func foldedTools(_ count: Int) -> String { "\(count) passos" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Mais recentes primeiro" : "Mais antigos primeiro"
    }
    func backlogNow(_ count: Int) -> String { "agora \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Caminho adicionado — o Claude Code lê" : "\(count) caminhos adicionados"
    }
    func stackConfirm(_ command: String) -> String { "Prima novamente para executar:  \(command)" }
    let hintStacks = "servidores"
    func stackTip(up: Int, total: Int) -> String { "\(up) de \(total) servidores ativos — ⌘S para a lista" }
    let stackTipUnknown = "Este projeto tem servidores; o seu comando de estado ainda não é fiável — ⌘S"
    let stackUntrusted = "sem aprovação"
    let stackActionStart = "iniciar"
    let stackActionRestart = "reiniciar"
    let stackActionStop = "parar"
    let stackActionLogs = "registo"
    let stackLogAll = "todos"
    let stackLogBack = "transcrição"
    let stackActionAllow = "permitir"
    let stackActionAgain = "de novo"
    func sessionTip(index: Int, total: Int) -> String { "Sessão \(index) de \(total) — ⌘K para mudar" }
    let sessionWaiting = "aguarda a sua resposta"
    let islandDone = "concluído"
    let islandAllSessions = "Todas as sessões…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) aguarda a sua resposta"
                          : "\(labels.count) sessões aguardam a sua resposta"
    }
    func statusWorking(_ count: Int) -> String { "\(count) em execução" }

    let settingsTitle = "Definições do Clawdline"
    let settingsGeneral = "Geral"
    let settingsBar = "A barra"
    let settingsReading = "Leitura"
    let settingsVoice = "Ditado"
    let settingsHotkey = "Atalho"
    let settingsRecording = "Prima teclas…"
    let settingsScope = "Ativo em"
    let settingsScopeGlobal = "Em todas as apps"
    let settingsScopeHint = "Bundle ids separados por vírgulas. Vazio significa em todo o lado."
    let settingsLanguage = "Idioma"
    let settingsReopen = "Voltar com o terminal"
    let settingsFollow = "Mudar também o separador do terminal"
    let settingsNotch = "Viver no entalhe"
    let settingsNotchHint = "Uma personagem na caixa da câmara. Desligado é desligado: nada é desenhado nem é criada janela."
    let settingsPosition = "Altura no ecrã"
    let settingsWidth = "Largura"
    let settingsOpacity = "Opacidade do cartão"
    let settingsImagesPaste = "Enviar as imagens como imagens"
    let settingsShow = "Mostrar"
    let settingsPaneHeight = "Altura do painel"
    let settingsTextSize = "Tamanho do texto"
    let settingsPaneFont = "Tipo de letra do painel"
    let settingsBlur = "Desfoque atrás"
    let settingsNewestFirst = "Mais recentes primeiro"
    let settingsEngine = "Reconhecimento"
    let settingsSettle = "Uma pausa termina a frase"
    let settingsStop = "Um silêncio termina a sessão"
    let settingsAuto = "Automático"
    let settingsTranscript = "Transcrição"
    let settingsTerminal = "Terminal"
    let settingsOff = "Desligado"
    let settingsOpenFile = "Abrir o ficheiro de configuração…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f s", value) }

    let menuOpen = "Abrir a barra"
    let menuReveal = "Ir para a aba de destino"
    let menuMascot = "Mascote"
    let menuLogin = "Abrir ao fazer login"
    let menuEditConfig = "Definições…"
    let menuReload = "Recarregar configuração"
    let menuQuit = "Sair do Clawdline"
    let menuNoTarget = "(ainda não detectado)"

    func hotkeyFailedTitle(_ combo: String) -> String { "Não foi possível registrar \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Provavelmente outro app já usa esse atalho — Spotlight, troca de teclado, \
        BetterTouchTool e afins.

        Escolha outro: edite "hotkey" em \(configPath) e depois escolha \
        "Recarregar configuração" na barra de menus.

        Até lá, o ✳ na barra de menus continua abrindo a barra.
        """
    }
    let loginFailed = "Não foi possível configurar a abertura ao fazer login"
}
