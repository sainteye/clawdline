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
    let settingsScopeHint = "Vacío significa en todas partes, así que quitar la última enciende el interruptor de arriba — y volver a apagarlo devuelve la lista. El archivo de configuración las guarda como identificadores de bundle, y una lista editada a mano sigue funcionando."
    let settingsScopeAdd = "Añadir una app…"
    let settingsScopeChoose = "Elegir una app…"
    let settingsScopeRunning = "Abiertas ahora mismo"
    let settingsScopeRemove = "Quitar"
    let settingsLanguage = "Idioma"
    let settingsReopen = "La barra va y viene con el terminal"
    let settingsReopenHint = "Salir del terminal guarda la barra, y volver a él la trae de vuelta. Esc es lo que quiere decir que has terminado con ella."
    let settingsFollow = "El terminal muestra aquello a lo que apunta la barra"
    let settingsFollowHint = "Selecciona la pestaña de esa sesión. No trae el terminal al frente: si lo hiciera, cada pulsación de Tab te sacaría el teclado de la casilla en la que estás escribiendo."
    let settingsNotch = "Vivir en la muesca"
    let settingsNotchHint = "Un personaje en la carcasa de la cámara. Desactivado es desactivado: no se dibuja nada ni se crea ventana."
    let settingsPosition = "Altura en pantalla"
    let settingsWidth = "Anchura"
    let settingsOpacity = "Opacidad de la tarjeta"
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
    let settingsStateHook = "Cuando una sesión cambia de estado"
    let settingsStateHookHint = "Cada vez que una sesión se pone a trabajar, termina o te pregunta algo, Clawdline ejecuta esto: tu propio programa, con los detalles en su entorno. Se escribe en el archivo de configuración y no aquí porque es un argv y no una línea de órdenes — una ruta con un espacio dentro tiene que seguir siendo una sola ruta."
    let settingsRemote = "Acceso remoto"
    let settingsRemoteServe = "Dejar que un navegador o tu teléfono vean tus sesiones"
    let settingsRemoteHint = "Desactivado, nada de fuera de este Mac llega a nada. Activado, la lista de sesiones se sirve en 127.0.0.1: para un navegador de aquí, para un teléfono al otro lado de un túnel, para un script. Lo que eso entrega son nombres de repositorios, ramas y títulos de tareas, y por eso sigue apagado hasta que digas lo contrario."
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
    let settingsTunnel = "Llegar a este Mac desde cualquier sitio"
    let settingsTunnelQuick = "Una dirección generada"
    let settingsTunnelNamed = "Mi propio dominio"
    let settingsTunnelHostname = "Nombre de host"
    let settingsTunnelHint = "Abre una conexión saliente a través de cloudflared: sin redirección de puertos y sin nada a la escucha en tu red. No arranca hasta que haya un dispositivo vinculado, porque detrás del túnel está el nombre de cada repositorio y el título de cada tarea de este Mac."
    let settingsRemoteWrite = "Dejar que un dispositivo vinculado escriba en una sesión"
    let settingsRemoteWriteHint = "Desactivado, un dispositivo vinculado solo puede leer. Activado, puede enviar texto a una sesión y abrir sesiones nuevas — lo que ejecuta código en este Mac, porque eso es lo que hace Claude Code. Es una decisión distinta de la de arriba, así que es un interruptor distinto."
    let settingsRemotePhone = "Vincular un teléfono…"
    let settingsRemotePhoneHint = "Muestra un código para escanear. Lleva una clave propia: una foto de ese código es un dispositivo que ves en esta lista y que puedes quitar cuando quieras — no la clave de este Mac."
    let pairingScanTitle = "Escanea esto con el teléfono"
    let pushWaiting = "espera una respuesta"
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
