import Foundation

struct Russian: Copy {
    let placeholder = "Сообщение для Claude Code…"

    let hintSend = "отправить"
    let hintNewline = "новая строка"
    let hintSwitch = "переключить"
    let hintList = "список"
    let hintMascot = "маскот"
    let hintOutput = "вывод"
    let hintFullscreen = "во весь экран"
    let hintKeys = "клавиши"
    let hintTextSize = "размер"
    let hintOrder = "обратный порядок"
    let hintVoice = "диктовка"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Слушает на этом Mac — нажмите ещё раз, чтобы остановить"
                 : "Слушает — этот язык распознаёт Apple, а не этот Mac"
    }
    let voiceNoPermission = "Диктовке нужен доступ к микрофону и распознаванию речи"
    let voiceUnavailable = "Диктовка сейчас недоступна"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper перечитывает… %.1f с", seconds)
    }
    let whisperMissing = "Whisper не установлен — см. docs/whisper.md"
    let whisperNothingHeard = "Ничего не услышал"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Диктовка: Apple, затем Whisper (\(model))"
        case .noBinary: return "Диктовка: только Apple — нет whisper-cli"
        case .noModel: return "Диктовка: только Apple — whisper-cli есть, модели нет"
        }
    }
    func voiceListeningWhisper() -> String {
        "Слушает — когда вы закончите, Whisper перечитает"
    }

    let scanning = "Поиск…"
    let noSession = "Сессия Claude Code не найдена"
    let nothingToSend = "Некуда отправлять — сначала запустите Claude Code в терминале"
    let sendFailed = "Не удалось отправить"
    let itermSilent = "iTerm2 не ответил"
    let scriptMissing = "Нет iterm.js — повреждённый бандл приложения?"
    let cannotList = "Не удалось прочитать сессии iTerm2"
    let noOutput = "В этой сессии пока нечего читать."
    func outputSize(_ pt: Int) -> String { "Размер текста вывода \(pt) pt — ⌘J, чтобы посмотреть" }
    func foldedTools(_ count: Int) -> String { "шагов: \(count)" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Сначала новые" : "Сначала старые"
    }
    func backlogNow(_ count: Int) -> String { "сейчас \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Путь добавлен — Claude Code его прочитает" : "Добавлено путей: \(count)"
    }

    let menuOpen = "Открыть строку ввода"
    let menuReveal = "Перейти к нужной вкладке"
    let menuMascot = "Маскот"
    let menuLogin = "Запускать при входе"
    let menuEditConfig = "Изменить настройки…"
    let menuReload = "Перечитать настройки"
    let menuQuit = "Выйти из Clawdline"
    let menuNoTarget = "(пока не найдено)"

    func hotkeyFailedTitle(_ combo: String) -> String { "Не удалось зарегистрировать \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Скорее всего, это сочетание уже занято другим приложением — Spotlight, \
        переключатель раскладки, BetterTouchTool и тому подобное.

        Выберите другое: измените "hotkey" в \(configPath), затем выберите \
        «Перечитать настройки» в строке меню.

        До тех пор строку ввода по-прежнему открывает ✳ в строке меню.
        """
    }
    let loginFailed = "Не удалось настроить запуск при входе"
}
