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
    func stackConfirm(_ command: String) -> String { "Нажмите ещё раз, чтобы выполнить:  \(command)" }
    let hintStacks = "серверы"
    func stackTip(up: Int, total: Int) -> String { "Запущено \(up) из \(total) серверов — ⌘S для списка" }
    let stackTipUnknown = "У проекта есть серверы, но его команда состояния ещё не доверена — ⌘S"
    let stackUntrusted = "не доверено"
    let stackActionStart = "запустить"
    let stackActionRestart = "перезапуск"
    let stackActionStop = "остановить"
    let stackActionLogs = "журнал"
    let stackLogAll = "все"
    let stackLogBack = "расшифровка"
    let stackActionAllow = "разрешить"
    let stackActionAgain = "ещё раз"
    func sessionTip(index: Int, total: Int) -> String { "Сессия \(index) из \(total) — ⌘K для переключения" }
    let sessionWaiting = "ждёт вашего ответа"
    let islandDone = "готово"
    let islandAllSessions = "Все сессии…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) ждёт вашего ответа"
                          : "\(labels.count) сессий ждут вашего ответа"
    }
    func statusWorking(_ count: Int) -> String { "\(count) выполняется" }

    let settingsTitle = "Настройки Clawdline"
    let settingsGeneral = "Основные"
    let settingsBar = "Строка"
    let settingsReading = "Чтение"
    let settingsVoice = "Диктовка"
    let settingsHotkey = "Сочетание клавиш"
    let settingsRecording = "Нажмите клавиши…"
    let settingsScope = "Работает в"
    let settingsScopeGlobal = "Во всех приложениях"
    let settingsScopeHint = "Bundle id через запятую. Пусто — везде."
    let settingsLanguage = "Язык"
    let settingsReopen = "Возвращаться вместе с терминалом"
    let settingsFollow = "Переключать и вкладку терминала"
    let settingsNotch = "Жить в вырезе"
    let settingsNotchHint = "Персонаж в корпусе камеры. Выключено — значит выключено: ничего не рисуется и окно не создаётся."
    let settingsPosition = "Высота на экране"
    let settingsWidth = "Ширина"
    let settingsOpacity = "Непрозрачность карточки"
    let settingsImagesPaste = "Отправлять изображения как изображения"
    let settingsShow = "Показывать"
    let settingsPaneHeight = "Высота панели"
    let settingsTextSize = "Размер текста"
    let settingsPaneFont = "Шрифт панели"
    let settingsBlur = "Размытие позади"
    let settingsNewestFirst = "Сначала новые"
    let settingsEngine = "Распознаватель"
    let settingsSettle = "Пауза завершает фразу"
    let settingsStop = "Тишина завершает сессию"
    let settingsAuto = "Авто"
    let settingsTranscript = "Расшифровка"
    let settingsTerminal = "Терминал"
    let settingsOff = "Выкл"
    let settingsHooks = "Хуки Claude Code"
    let settingsHooksHint = "Когда они установлены, Claude Code сам сообщает момент, когда ход начался, закончился или ждёт ответа, — вместо того чтобы Clawdline узнал об этом при следующем опросе. Читается всё по-прежнему с экрана; здесь решается только, насколько быстро."
    let settingsHooksInstall = "Установить"
    let settingsHooksRemove = "Убрать"
    let settingsHooksOff = "Не установлены — состояние читается только с экрана"
    let settingsHooksOn = "Установлены — ни одна сессия ещё не отозвалась"
    let settingsHooksLive = "Установлены, сессии отзываются"
    let settingsOpenFile = "Открыть файл настроек…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f с", value) }

    let menuOpen = "Открыть строку ввода"
    let menuReveal = "Перейти к нужной вкладке"
    let menuMascot = "Маскот"
    let menuLogin = "Запускать при входе"
    let menuEditConfig = "Настройки…"
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
