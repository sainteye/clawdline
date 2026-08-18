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
    func stackConfirm(_ command: String) -> String { "Çalıştırmak için tekrar basın:  \(command)" }
    let hintStacks = "sunucular"
    func stackTip(up: Int, total: Int) -> String { "\(total) sunucudan \(up) tanesi çalışıyor — liste için ⌘S" }
    let stackTipUnknown = "Bu projenin sunucuları var ama durum komutu henüz güvenilir değil — ⌘S"
    let stackUntrusted = "onaylanmadı"
    let stackActionStart = "başlat"
    let stackActionRestart = "yeniden başlat"
    let stackActionStop = "durdur"
    let stackActionLogs = "günlük"
    let stackLogAll = "tümü"
    let stackLogBack = "döküm"
    let stackActionAllow = "izin ver"
    let stackActionAgain = "tekrar bas"
    func sessionTip(index: Int, total: Int) -> String { "\(total) oturumdan \(index). — değiştirmek için ⌘K" }
    let sessionWaiting = "yanıtını bekliyor"
    let islandDone = "bitti"
    let islandAllSessions = "Tüm oturumlar…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) yanıtını bekliyor"
                          : "\(labels.count) oturum yanıtını bekliyor"
    }
    func statusWorking(_ count: Int) -> String { "\(count) çalışıyor" }

    let settingsTitle = "Clawdline Ayarları"
    let settingsGeneral = "Genel"
    let settingsBar = "Çubuk"
    let settingsReading = "Okuma"
    let settingsVoice = "Dikte"
    let settingsHotkey = "Kısayol"
    let settingsRecording = "Tuşlara bas…"
    let settingsScope = "Şurada çalışır"
    let settingsScopeGlobal = "Her uygulamada"
    let settingsScopeHint = "Bundle kimlikleri, virgülle. Boşsa her yerde."
    let settingsLanguage = "Dil"
    let settingsReopen = "Terminalle birlikte geri gel"
    let settingsFollow = "Terminaldeki sekmeyi de değiştir"
    let settingsNotch = "Çentikte yaşa"
    let settingsNotchHint = "Kamera yuvasında bir karakter. Kapalı gerçekten kapalı: hiçbir şey çizilmez, pencere açılmaz."
    let settingsPosition = "Ekrandaki yükseklik"
    let settingsWidth = "Genişlik"
    let settingsOpacity = "Kart opaklığı"
    let settingsImagesPaste = "Görselleri görsel olarak gönder"
    let settingsShow = "Göster"
    let settingsPaneHeight = "Panel yüksekliği"
    let settingsTextSize = "Yazı boyutu"
    let settingsPaneFont = "Panel yazı tipi"
    let settingsBlur = "Arkadaki bulanıklık"
    let settingsNewestFirst = "Önce en yeni"
    let settingsEngine = "Tanıyıcı"
    let settingsSettle = "Bir duraklama cümleyi bitirir"
    let settingsStop = "Bir sessizlik oturumu bitirir"
    let settingsAuto = "Otomatik"
    let settingsTranscript = "Döküm"
    let settingsTerminal = "Terminal"
    let settingsOff = "Kapalı"
    let settingsHooks = "Claude Code kancaları"
    let settingsHooksHint = "Kurulduğunda Claude Code bir turun başladığı, bittiği ya da yanıt beklediği anı kendisi haber verir; Clawdline'ın bir sonraki bakışını beklemez. Okuma yine ekrandan yapılır, burada belirlenen yalnızca ne kadar çabuk olduğu."
    let settingsHooksInstall = "Kur"
    let settingsHooksRemove = "Kaldır"
    let settingsHooksOff = "Kurulu değil — durum yalnızca ekrandan okunuyor"
    let settingsHooksOn = "Kurulu — henüz hiçbir oturum haber vermedi"
    let settingsHooksLive = "Kurulu, oturumlar haber veriyor"
    let settingsOpenFile = "Yapılandırma dosyasını aç…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f sn", value) }

    let menuOpen = "Giriş çubuğunu aç"
    let menuReveal = "Hedef sekmeye git"
    let menuMascot = "Maskot"
    let menuLogin = "Girişte başlat"
    let menuEditConfig = "Ayarlar…"
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
