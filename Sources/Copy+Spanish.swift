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

    let menuOpen = "Abrir la barra"
    let menuReveal = "Ir a la pestaña destino"
    let menuMascot = "Mascota"
    let menuLogin = "Abrir al iniciar sesión"
    let menuEditConfig = "Editar configuración…"
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
