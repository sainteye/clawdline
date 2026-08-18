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
    let settingsHooks = "Hooks do Claude Code"
    let settingsHooksHint = "Com eles instalados, o Claude Code avisa no instante em que um turno começa, termina ou espera por uma resposta, em vez de o Clawdline descobrir na verificação seguinte. Tudo continua a ser lido do ecrã; isto só decide com que rapidez."
    let settingsHooksInstall = "Instalar"
    let settingsHooksRemove = "Remover"
    let settingsHooksOff = "Não instalados — tudo é lido do ecrã"
    let settingsHooksOn = "Instalados — nenhuma sessão avisou ainda"
    let settingsHooksLive = "Instalados, e as sessões estão a avisar"
    let settingsRemote = "Acesso remoto"
    let settingsRemoteServe = "Responder por HTTP"
    let settingsRemoteHint = "Publica a lista de sessões em 127.0.0.1 para que um navegador, um celular do outro lado de um túnel ou um script consigam ler. Fica desligado até você ligar: um socket à escuta entrega nomes de repositórios, branches e títulos das tarefas."
    let settingsRemoteDevices = "Dispositivos pareados"
    let settingsRemoteNoDevices = "Nenhum ainda — fora deste Mac ninguém consegue ler nada"
    let settingsRemoteRevokeAll = "Desconectar tudo"
    let settingsRemoteOpen = "Abrir no navegador"
    let pairingIgnore = "Ignorar"
    func pairingAsks(_ device: String) -> String { "\(device) quer parear com este Mac" }
    func pairingCode(_ code: String) -> String {
        """
        Digite este código nele:

        \(code)

        Vale por dois minutos. Se não foi você que acabou de pedir, ignore — sem este código, \
        quem pediu não consegue concluir.
        """
    }
    let settingsTunnel = "Acessível de fora"
    let settingsTunnelQuick = "Um endereço gerado"
    let settingsTunnelNamed = "Meu próprio domínio"
    let settingsTunnelHostname = "Nome do host"
    let settingsTunnelHint = "Abre uma conexão de saída através do cloudflared — sem redirecionamento de portas, nada à escuta na sua rede. Não começa enquanto não houver um dispositivo pareado, porque atrás do túnel está o nome de cada repositório e o título de cada tarefa deste Mac."
    let settingsRemoteWrite = "Deixar os dispositivos pareados escrever"
    let settingsRemoteWriteHint = "Desligado, um dispositivo pareado só consegue ler. Ligado, ele pode mandar texto para dentro de uma sessão e abrir sessões novas — o que executa código neste Mac, porque é isso que o Claude Code faz. É uma decisão diferente da de cima, então é um interruptor separado."
    let settingsRemotePhone = "Parear um celular…"
    let settingsRemotePhoneHint = "Mostra um código para escanear. Ele carrega uma chave só dele: uma foto desse código é um dispositivo que você vê nesta lista e pode tirar de novo — não a chave deste Mac."
    let pairingScanTitle = "Escaneie isto com o celular"
    let pushWaiting = "está esperando uma resposta"
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
