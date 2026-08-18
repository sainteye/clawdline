import Foundation

struct Spanish: Copy {
    let placeholder = "Escribe a Claude Code…"

    let hintSend = "enviar"
    let hintNewline = "nueva línea"
    let hintSwitch = "cambiar"
    let hintList = "lista"
    let hintMascot = "mascota"
    let hintOutput = "salida"
    let hintFullscreen = "pantalla completa"
    let hintKeys = "teclas"
    let hintTextSize = "tamaño"
    let hintOrder = "invertir"
    let hintVoice = "dictar"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Escuchando en este Mac — pulsa otra vez para parar"
                 : "Escuchando — este idioma lo transcribe Apple, no este Mac"
    }
    let voiceNoPermission = "El dictado necesita acceso al micrófono y al reconocimiento de voz"
    let voiceUnavailable = "El dictado no está disponible ahora mismo"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper lo está releyendo… %.1f s", seconds)
    }
    let whisperMissing = "Whisper no está instalado — consulta docs/whisper.md"
    let whisperNothingHeard = "No se oyó nada"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Dictado: Apple y después Whisper (\(model))"
        case .noBinary: return "Dictado: solo Apple — falta whisper-cli"
        case .noModel: return "Dictado: solo Apple — whisper-cli está, falta el modelo"
        }
    }
    func voiceListeningWhisper() -> String {
        "Escuchando — Whisper lo revisa otra vez cuando pares"
    }

    let scanning = "Buscando…"
    let noSession = "No se encontró ninguna sesión de Claude Code"
    let nothingToSend = "No hay destino — abre antes Claude Code en una terminal"
    let sendFailed = "No se pudo enviar"
    let itermSilent = "iTerm2 no respondió"
    let scriptMissing = "Falta iterm.js — ¿paquete de la app dañado?"
    let cannotList = "No se pudieron leer las sesiones de iTerm2"
    let noOutput = "Todavía no hay nada que leer en esta sesión."
    func outputSize(_ pt: Int) -> String { "Texto de salida \(pt) pt — ⌘J para verlo" }
    func foldedTools(_ count: Int) -> String { "\(count) pasos" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Lo más reciente primero" : "Lo más antiguo primero"
    }
    func backlogNow(_ count: Int) -> String { "ahora \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Ruta añadida — Claude Code la lee" : "Se añadieron \(count) rutas"
    }
    func stackConfirm(_ command: String) -> String { "Pulsa otra vez para ejecutar:  \(command)" }
    let hintStacks = "servidores"
    func stackTip(up: Int, total: Int) -> String { "\(up) de \(total) servidores activos — ⌘S para la lista" }
    let stackTipUnknown = "Este proyecto tiene servidores; su comando de estado aún no es de confianza — ⌘S"
    let stackUntrusted = "sin aprobar"
    let stackActionStart = "iniciar"
    let stackActionRestart = "reiniciar"
    let stackActionStop = "detener"
    let stackActionLogs = "registro"
    let stackLogAll = "todos"
    let stackLogBack = "transcripción"
    let stackActionAllow = "permitir"
    let stackActionAgain = "otra vez"
    func sessionTip(index: Int, total: Int) -> String { "Sesión \(index) de \(total) — ⌘K para cambiar" }
    let sessionWaiting = "te está esperando"
    let islandDone = "listo"
    let islandAllSessions = "Todas las sesiones…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) te está esperando"
                          : "\(labels.count) sesiones te están esperando"
    }
    func statusWorking(_ count: Int) -> String { "\(count) en marcha" }

    let settingsTitle = "Ajustes de Clawdline"
    let settingsGeneral = "General"
    let settingsBar = "La barra"
    let settingsReading = "Lectura"
    let settingsVoice = "Dictado"
    let settingsHotkey = "Atajo"
    let settingsRecording = "Pulsa unas teclas…"
    let settingsScope = "Se activa en"
    let settingsScopeGlobal = "En todas las apps"
    let settingsScopeHint = "Identificadores de bundle separados por comas. Vacío significa en todas."
    let settingsLanguage = "Idioma"
    let settingsReopen = "Volver con el terminal"
    let settingsFollow = "Mover también la pestaña del terminal"
    let settingsNotch = "Vivir en la muesca"
    let settingsNotchHint = "Un personaje en la carcasa de la cámara. Desactivado es desactivado: no se dibuja nada ni se crea ventana."
    let settingsPosition = "Altura en pantalla"
    let settingsWidth = "Anchura"
    let settingsOpacity = "Opacidad de la tarjeta"
    let settingsImagesPaste = "Enviar las imágenes como imágenes"
    let settingsShow = "Mostrar"
    let settingsPaneHeight = "Altura del panel"
    let settingsTextSize = "Tamaño del texto"
    let settingsPaneFont = "Fuente del panel"
    let settingsBlur = "Desenfoque detrás"
    let settingsNewestFirst = "Lo más reciente arriba"
    let settingsEngine = "Reconocedor"
    let settingsSettle = "Una pausa termina la frase"
    let settingsStop = "Un silencio termina la sesión"
    let settingsAuto = "Automático"
    let settingsTranscript = "Transcripción"
    let settingsTerminal = "Terminal"
    let settingsOff = "Desactivado"
    let settingsHooks = "Hooks de Claude Code"
    let settingsHooksHint = "Con ellos instalados, Claude Code avisa en el instante en que un turno empieza, termina o espera una respuesta, en lugar de que Clawdline se entere en la siguiente comprobación. Todo se sigue leyendo de la pantalla; esto solo decide con qué rapidez."
    let settingsHooksInstall = "Instalar"
    let settingsHooksRemove = "Quitar"
    let settingsHooksOff = "Sin instalar — todo se lee de la pantalla"
    let settingsHooksOn = "Instalados — ninguna sesión ha avisado todavía"
    let settingsHooksLive = "Instalados, y las sesiones avisan"
    let settingsRemote = "Acceso remoto"
    let settingsRemoteServe = "Responder por HTTP"
    let settingsRemoteHint = "Publica la lista de sesiones en 127.0.0.1 para que la puedan leer un navegador, un teléfono al otro lado de un túnel o un script. Desactivado mientras no lo actives: un socket a la escucha entrega los nombres de los repositorios, las ramas y los títulos de las tareas."
    let settingsRemoteDevices = "Dispositivos vinculados"
    let settingsRemoteNoDevices = "Ninguno todavía — fuera de este Mac nadie puede leer nada"
    let settingsRemoteRevokeAll = "Desconectar todo"
    let settingsRemoteOpen = "Abrir en el navegador"
    let pairingIgnore = "Ignorar"
    func pairingAsks(_ device: String) -> String { "\(device) quiere vincularse con este Mac" }
    func pairingCode(_ code: String) -> String {
        """
        Escribe este código en ese dispositivo:

        \(code)

        Vale dos minutos. Si no acabas de pedirlo tú, ignóralo — sin este código, quien lo \
        pidió no puede terminar.
        """
    }
    let settingsTunnel = "Accesible desde fuera"
    let settingsTunnelQuick = "Una dirección generada"
    let settingsTunnelNamed = "Mi propio dominio"
    let settingsTunnelHostname = "Nombre de host"
    let settingsTunnelHint = "Abre una conexión saliente a través de cloudflared: sin redirección de puertos y sin nada a la escucha en tu red. No arranca hasta que haya un dispositivo vinculado, porque detrás del túnel está el nombre de cada repositorio y el título de cada tarea de este Mac."
    let settingsRemoteWrite = "Dejar escribir a los dispositivos vinculados"
    let settingsRemoteWriteHint = "Desactivado, un dispositivo vinculado solo puede leer. Activado, puede enviar texto a una sesión y abrir sesiones nuevas — lo que ejecuta código en este Mac, porque eso es lo que hace Claude Code. Es una decisión distinta de la de arriba, así que es un interruptor distinto."
    let settingsOpenFile = "Abrir el archivo de configuración…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f s", value) }

    let menuOpen = "Abrir la barra"
    let menuReveal = "Ir a la pestaña destino"
    let menuMascot = "Mascota"
    let menuLogin = "Abrir al iniciar sesión"
    let menuEditConfig = "Ajustes…"
    let menuReload = "Recargar configuración"
    let menuQuit = "Salir de Clawdline"
    let menuNoTarget = "(aún sin detectar)"

    func hotkeyFailedTitle(_ combo: String) -> String { "No se pudo registrar \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Lo más probable es que otra app ya la use — Spotlight, un cambiador de teclado, \
        BetterTouchTool y demás.

        Elige otra: edita "hotkey" en \(configPath) y luego elige \
        "Recargar configuración" en la barra de menús.

        Mientras tanto, el ✳ de la barra de menús sigue abriendo la barra.
        """
    }
    let loginFailed = "No se pudo configurar el arranque al iniciar sesión"
}
