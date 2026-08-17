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

    let menuOpen = "입력창 열기"
    let menuReveal = "대상 탭으로 이동"
    let menuMascot = "마스코트"
    let menuLogin = "로그인 시 실행"
    let menuEditConfig = "설정 편집…"
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
