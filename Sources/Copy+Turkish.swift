import Foundation

struct Turkish: Copy {
    let placeholder = "Claude Code'a yaz…"

    let hintSend = "gönder"
    let hintNewline = "yeni satır"
    let hintSwitch = "değiştir"
    let hintList = "liste"
    let hintMascot = "maskot"
    let hintOutput = "çıktı"
    let hintFullscreen = "tam ekran"
    let hintKeys = "tuşlar"
    let hintTextSize = "yazı boyutu"
    let hintOrder = "ters sıra"
    let hintVoice = "dikte"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Bu Mac'te dinliyor — durdurmak için tekrar bas"
                 : "Dinliyor — bu dili Apple yazıya döküyor, bu Mac değil"
    }
    let voiceNoPermission = "Dikte için mikrofon ve konuşma tanıma izni gerekiyor"
    let voiceUnavailable = "Dikte şu anda kullanılamıyor"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper yeniden okuyor… %.1f sn", seconds)
    }
    let whisperMissing = "Whisper kurulu değil — docs/whisper.md'ye bak"
    let whisperNothingHeard = "Hiçbir şey duyulmadı"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Dikte: önce Apple, sonra Whisper (\(model))"
        case .noBinary: return "Dikte: yalnızca Apple — whisper-cli yok"
        case .noModel: return "Dikte: yalnızca Apple — whisper-cli var, model yok"
        }
    }
    func voiceListeningWhisper() -> String {
        "Dinliyor — sen durunca Whisper bir kez daha bakıyor"
    }

    let scanning = "Aranıyor…"
    let noSession = "Claude Code oturumu bulunamadı"
    let nothingToSend = "Gönderilecek yer yok — önce bir terminalde Claude Code başlat"
    let sendFailed = "Gönderilemedi"
    let itermSilent = "iTerm2 yanıt vermedi"
    let scriptMissing = "iterm.js yok — uygulama paketi bozuk mu?"
    let cannotList = "iTerm2 oturumları okunamadı"
    let noOutput = "Bu oturumda okunacak bir şey henüz yok."
    func outputSize(_ pt: Int) -> String { "Çıktı yazı boyutu \(pt) pt — görmek için ⌘J" }
    func foldedTools(_ count: Int) -> String { "\(count) adım" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "En yeni üstte" : "En eski üstte"
    }
    func backlogNow(_ count: Int) -> String { "şimdi \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Yol eklendi — Claude Code onu okur" : "\(count) yol eklendi"
    }

    let menuOpen = "Giriş çubuğunu aç"
    let menuReveal = "Hedef sekmeye git"
    let menuMascot = "Maskot"
    let menuLogin = "Girişte başlat"
    let menuEditConfig = "Ayarları düzenle…"
    let menuReload = "Ayarları yeniden yükle"
    let menuQuit = "Clawdline'dan çık"
    let menuNoTarget = "(henüz bulunamadı)"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) kaydedilemedi" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Büyük ihtimalle başka bir uygulama almış — Spotlight, klavye düzeni değiştirici, \
        BetterTouchTool ve benzerleri.

        Başka birini seç: \(configPath) içindeki "hotkey" değerini düzenle, sonra menü \
        çubuğundan "Ayarları yeniden yükle" seçeneğini seç.

        O zamana kadar menü çubuğundaki ✳ giriş çubuğunu açmayı sürdürür.
        """
    }
    let loginFailed = "Girişte başlatma ayarlanamadı"
}
