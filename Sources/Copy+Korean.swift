import Foundation

struct Korean: Copy {
    let placeholder = "Claude Code에 메시지…"

    let hintSend = "보내기"
    let hintNewline = "줄바꿈"
    let hintSwitch = "전환"
    let hintList = "목록"
    let hintMascot = "마스코트"
    let hintOutput = "출력"
    let hintFullscreen = "전체 화면"
    let hintKeys = "단축키"
    let hintTextSize = "글자 크기"
    let hintOrder = "역순"
    let hintVoice = "받아쓰기"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "이 Mac에서 듣는 중 — 다시 누르면 중지"
                 : "듣는 중 — 이 언어는 이 Mac이 아니라 Apple이 인식합니다"
    }
    let voiceNoPermission = "받아쓰기에는 마이크와 음성 인식 권한이 필요합니다"
    let voiceUnavailable = "지금은 받아쓰기를 사용할 수 없습니다"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper가 다시 듣는 중… %.1f초", seconds)
    }
    let whisperMissing = "Whisper가 설치되어 있지 않습니다 — docs/whisper.md 참고"
    let whisperNothingHeard = "아무것도 들리지 않았습니다"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "받아쓰기: Apple 다음 Whisper (\(model))"
        case .noBinary: return "받아쓰기: Apple만 — whisper-cli 없음"
        case .noModel: return "받아쓰기: Apple만 — whisper-cli는 있고 모델이 없음"
        }
    }
    func voiceListeningWhisper() -> String { "듣는 중 — 멈추면 Whisper가 한 번 더 봅니다" }

    let scanning = "찾는 중…"
    let noSession = "Claude Code 세션을 찾지 못했습니다"
    let nothingToSend = "보낼 곳이 없습니다 — 먼저 터미널에서 Claude Code를 실행하세요"
    let sendFailed = "보내지 못했습니다"
    let itermSilent = "iTerm2가 응답하지 않았습니다"
    let scriptMissing = "iterm.js가 없습니다 — 앱 번들이 손상되었을 수 있습니다"
    let cannotList = "iTerm2 세션을 읽지 못했습니다"
    let noOutput = "이 세션에서 아직 읽을 것이 없습니다."
    func outputSize(_ pt: Int) -> String { "출력 글자 크기 \(pt)pt — ⌘J로 보기" }
    func foldedTools(_ count: Int) -> String { "\(count)단계" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "최신순" : "오래된순"
    }
    func backlogNow(_ count: Int) -> String { "현재 \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "경로를 넣었습니다 — Claude Code가 읽습니다" : "경로 \(count)개를 넣었습니다"
    }
    func stackConfirm(_ command: String) -> String { "다시 누르면 실행합니다: \(command)" }
    let hintStacks = "서버"
    func stackTip(up: Int, total: Int) -> String { "서버 \(total)개 중 \(up)개 실행 중 — ⌘S로 목록 보기" }
    let stackTipUnknown = "이 프로젝트에는 서버가 있지만 status 명령이 아직 신뢰되지 않았습니다 — ⌘S"
    let stackUntrusted = "미승인"
    let stackActionStart = "시작"
    let stackActionRestart = "재시작"
    let stackActionStop = "중지"
    let stackActionLogs = "로그"
    let stackLogAll = "전체"
    let stackLogBack = "대화 기록"
    let stackActionAllow = "허용"
    let stackActionAgain = "다시 누르기"
    func sessionTip(index: Int, total: Int) -> String { "\(total)개 중 \(index)번째 세션 — ⌘K로 전환" }
    let sessionWaiting = "답변 대기 중"
    let islandDone = "완료"
    let islandAllSessions = "모든 세션…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) 세션이 답변을 기다립니다"
                          : "\(labels.count)개 세션이 답변을 기다립니다"
    }
    func statusWorking(_ count: Int) -> String { "\(count)개 실행 중" }

    let settingsTitle = "Clawdline 설정"
    let settingsGeneral = "일반"
    let settingsBar = "막대"
    let settingsReading = "읽기"
    let settingsVoice = "받아쓰기"
    let settingsHotkey = "단축키"
    let settingsRecording = "키를 누르세요…"
    let settingsScope = "작동하는 곳"
    let settingsScopeGlobal = "모든 앱에서"
    let settingsScopeHint = "번들 ID를 쉼표로 구분. 비우면 어디서나."
    let settingsLanguage = "언어"
    let settingsReopen = "터미널과 함께 돌아오기"
    let settingsFollow = "터미널 탭도 함께 전환"
    let settingsNotch = "노치에 살기"
    let settingsNotchHint = "카메라 하우징에 사는 캐릭터. 끄면 끝 — 아무것도 그리지 않고 창도 만들지 않습니다."
    let settingsPosition = "화면에서의 높이"
    let settingsWidth = "너비"
    let settingsOpacity = "카드 불투명도"
    let settingsImagesPaste = "이미지를 이미지로 보내기"
    let settingsShow = "표시"
    let settingsPaneHeight = "패널 높이"
    let settingsTextSize = "글자 크기"
    let settingsPaneFont = "패널 글꼴"
    let settingsBlur = "뒤쪽 흐림"
    let settingsNewestFirst = "최신 항목 먼저"
    let settingsEngine = "인식기"
    let settingsSettle = "이만큼 쉬면 문장이 끝남"
    let settingsStop = "이만큼 조용하면 세션 종료"
    let settingsAuto = "자동"
    let settingsTranscript = "트랜스크립트"
    let settingsTerminal = "터미널"
    let settingsOff = "끔"
    let settingsHooks = "Claude Code 훅"
    let settingsHooksHint = "설치해 두면 한 차례가 시작되거나 끝나거나 답을 기다리는 순간을 Claude Code가 곧바로 알려 줍니다. Clawdline이 다음에 확인할 때까지 기다리지 않습니다. 상태를 읽는 곳은 여전히 화면이고, 여기서 정해지는 것은 얼마나 빨리인지뿐입니다."
    let settingsHooksInstall = "설치"
    let settingsHooksRemove = "제거"
    let settingsHooksOff = "설치 안 됨 — 상태는 모두 화면에서 읽습니다"
    let settingsHooksOn = "설치됨 — 아직 알려 온 세션이 없습니다"
    let settingsHooksLive = "설치됨. 세션이 알려 오고 있습니다"
    let settingsOpenFile = "설정 파일 열기…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f초", value) }

    let menuOpen = "입력창 열기"
    let menuReveal = "대상 탭으로 이동"
    let menuMascot = "마스코트"
    let menuLogin = "로그인 시 실행"
    let menuEditConfig = "설정…"
    let menuReload = "설정 다시 읽기"
    let menuQuit = "Clawdline 종료"
    let menuNoTarget = "(아직 찾지 못함)"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo)를 등록하지 못했습니다" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        다른 앱이 이미 쓰고 있을 가능성이 큽니다 — Spotlight, 입력기 전환, \
        BetterTouchTool 등.

        다른 키를 고르세요: \(configPath)의 "hotkey"를 편집한 뒤 \
        메뉴 막대에서 "설정 다시 읽기"를 선택합니다.

        그때까지도 메뉴 막대의 ✳로 입력창을 열 수 있습니다.
        """
    }
    let loginFailed = "로그인 시 실행을 설정하지 못했습니다"
}
