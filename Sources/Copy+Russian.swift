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
    let settingsScopeHint = "Пусто — значит везде, поэтому если убрать последнее, включится переключатель выше, а если выключить его обратно, список вернётся. В файле настроек это bundle id, и список, поправленный вручную, тоже работает."
    let settingsScopeAdd = "Добавить приложение…"
    let settingsScopeChoose = "Выбрать приложение…"
    let settingsScopeRunning = "Открыты прямо сейчас"
    let settingsScopeRemove = "Убрать"
    let settingsLanguage = "Язык"
    let settingsReopen = "Строка приходит и уходит вместе с терминалом"
    let settingsReopenHint = "Ушли из терминала — строка убирается, вернулись в него — строка возвращается. А Esc означает, что вы с ней закончили."
    let settingsFollow = "Терминал показывает то, на что нацелена строка"
    let settingsFollowHint = "Выбирает вкладку этой сессии. Терминал на передний план не выводится: иначе каждое нажатие Tab уводило бы клавиатуру из поля, в котором вы печатаете."
    let settingsNotch = "Жить в вырезе"
    let settingsNotchHint = "Персонаж в корпусе камеры. Выключено — значит выключено: ничего не рисуется и окно не создаётся."
    let settingsPosition = "Высота на экране"
    let settingsWidth = "Ширина"
    let settingsOpacity = "Непрозрачность карточки"
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
    let settingsStateHook = "Когда сессия меняет состояние"
    let settingsStateHookHint = "Каждый раз, когда сессия берётся за работу, заканчивает или о чём-то вас спрашивает, Clawdline запускает это: вашу собственную программу, с подробностями в её окружении. Задаётся в файле настроек, а не здесь, потому что это argv, а не командная строка — путь с пробелом должен остаться одним путём."
    let settingsRemote = "Удалённый доступ"
    let settingsRemoteServe = "Разрешить браузеру или телефону видеть ваши сессии"
    let settingsRemoteHint = "Выключено — снаружи этого Mac ничто ни до чего не дотянется. Включено — список сессий отдаётся на 127.0.0.1: браузеру здесь, телефону через туннель, скрипту. Отдаются при этом имена репозиториев, ветки и названия задач, поэтому оно так и стоит выключенным, пока вы не скажете иначе."
    let settingsRemoteDevices = "Сопряжённые устройства"
    let settingsRemoteNoDevices = "Пока ни одного — за пределами этого Mac ничего прочитать нельзя"
    let settingsRemoteRevokeAll = "Отключить всё"
    let settingsRemoteOpen = "Открыть в браузере"
    let pairingIgnore = "Игнорировать"
    func pairingAsks(_ device: String) -> String { "\(device) хочет создать пару с этим Mac" }
    func pairingCode(_ code: String) -> String {
        """
        Введите этот код на нём:

        \(code)

        Он действует две минуты. Если вы только что этого не запрашивали — не обращайте \
        внимания: без этого кода тот, кто запросил, ничего не завершит.
        """
    }
    let settingsTunnel = "Дотянуться до этого Mac откуда угодно"
    let settingsTunnelQuick = "Сгенерированный адрес"
    let settingsTunnelNamed = "Мой собственный домен"
    let settingsTunnelHostname = "Имя хоста"
    let settingsTunnelHint = "Открывает исходящее соединение через cloudflared — никакого проброса портов, ничего слушающего в вашей сети. Не запустится, пока не сопряжено хотя бы одно устройство: за туннелем — имя каждого репозитория и название каждой задачи на этом Mac."
    let settingsRemoteWrite = "Разрешить сопряжённому устройству писать в сессию"
    let settingsRemoteWriteHint = "Выключено — сопряжённое устройство может только читать. Включено — оно может отправлять текст в сессию и запускать новые, а это выполняет код на этом Mac, потому что Claude Code именно этим и занимается. Решение здесь другое, чем выше, поэтому и переключатель отдельный."
    let settingsRemotePhone = "Сопрячь телефон…"
    let settingsRemotePhoneHint = "Показывает код для сканирования. У кода собственный ключ: снимок этого кода — это устройство, которое видно в списке выше и которое можно отключить, а не ключ самого этого Mac."
    let pairingScanTitle = "Наведите на это телефон"
    let pushWaiting = "ждёт ответа"
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
