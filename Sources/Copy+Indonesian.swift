import Foundation

struct Indonesian: Copy {
    let placeholder = "Tulis ke Claude Code…"

    let hintSend = "kirim"
    let hintNewline = "baris baru"
    let hintSwitch = "ganti"
    let hintList = "daftar"
    let hintMascot = "maskot"
    let hintOutput = "keluaran"
    let hintFullscreen = "layar penuh"
    let hintKeys = "tombol"
    let hintTextSize = "ukuran teks"
    let hintOrder = "balik urutan"
    let hintVoice = "dikte"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Mendengarkan di Mac ini — tekan lagi untuk berhenti"
                 : "Mendengarkan — bahasa ini ditranskripsi oleh Apple, bukan di Mac ini"
    }
    let voiceNoPermission = "Dikte memerlukan izin mikrofon dan pengenalan suara"
    let voiceUnavailable = "Dikte sedang tidak tersedia"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper sedang membacanya ulang… %.1f dtk", seconds)
    }
    let whisperMissing = "Whisper belum terpasang — lihat docs/whisper.md"
    let whisperNothingHeard = "Tidak terdengar apa pun"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Dikte: Apple, lalu Whisper (\(model))"
        case .noBinary: return "Dikte: hanya Apple — tidak ada whisper-cli"
        case .noModel: return "Dikte: hanya Apple — whisper-cli ada, modelnya belum"
        }
    }
    func voiceListeningWhisper() -> String {
        "Mendengarkan — Whisper membaca ulang begitu kamu berhenti"
    }

    let scanning = "Mencari…"
    let noSession = "Tidak ada sesi Claude Code yang ditemukan"
    let nothingToSend = "Tidak ada tujuan — jalankan dulu Claude Code di terminal"
    let sendFailed = "Gagal mengirim"
    let itermSilent = "iTerm2 tidak merespons"
    let scriptMissing = "iterm.js tidak ada — bundel aplikasi rusak?"
    let cannotList = "Tidak bisa membaca sesi iTerm2"
    let noOutput = "Belum ada yang bisa dibaca dari sesi ini."
    func outputSize(_ pt: Int) -> String { "Ukuran teks keluaran \(pt) pt — ⌘J untuk melihat" }
    func foldedTools(_ count: Int) -> String { "\(count) langkah" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Terbaru di atas" : "Terlama di atas"
    }
    func backlogNow(_ count: Int) -> String { "sekarang \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Path ditambahkan — Claude Code yang membacanya" : "\(count) path ditambahkan"
    }
    func stackConfirm(_ command: String) -> String { "Tekan lagi untuk menjalankan:  \(command)" }
    let hintStacks = "server"
    func stackTip(up: Int, total: Int) -> String { "\(up) dari \(total) server aktif — ⌘S untuk daftarnya" }
    let stackTipUnknown = "Proyek ini punya server, tetapi perintah status-nya belum dipercaya — ⌘S"
    let stackUntrusted = "belum dipercaya"
    let stackActionStart = "mulai"
    let stackActionRestart = "mulai ulang"
    let stackActionStop = "hentikan"
    let stackActionLogs = "log"
    let stackLogAll = "semua"
    let stackLogBack = "transkrip"
    let stackActionAllow = "izinkan"
    let stackActionAgain = "tekan lagi"
    func sessionTip(index: Int, total: Int) -> String { "Sesi \(index) dari \(total) — ⌘K untuk berpindah" }
    let sessionWaiting = "menunggu jawabanmu"
    let islandDone = "selesai"
    let islandAllSessions = "Semua sesi…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) menunggu jawabanmu"
                          : "\(labels.count) sesi menunggu jawabanmu"
    }
    func statusWorking(_ count: Int) -> String { "\(count) sedang berjalan" }

    let settingsTitle = "Pengaturan Clawdline"
    let settingsGeneral = "Umum"
    let settingsBar = "Bilah"
    let settingsReading = "Membaca"
    let settingsVoice = "Dikte"
    let settingsHotkey = "Pintasan"
    let settingsRecording = "Tekan tombol…"
    let settingsScope = "Aktif di"
    let settingsScopeGlobal = "Di semua aplikasi"
    let settingsScopeHint = "Bundle id, dipisah koma. Kosong berarti di mana saja."
    let settingsLanguage = "Bahasa"
    let settingsReopen = "Kembali bersama terminal"
    let settingsFollow = "Pindahkan juga tab terminal"
    let settingsNotch = "Tinggal di poni layar"
    let settingsNotchHint = "Karakter di rumah kamera. Mati berarti mati — tidak digambar, tidak ada jendela."
    let settingsPosition = "Tinggi di layar"
    let settingsWidth = "Lebar"
    let settingsOpacity = "Opasitas kartu"
    let settingsImagesPaste = "Kirim gambar sebagai gambar"
    let settingsShow = "Tampilkan"
    let settingsPaneHeight = "Tinggi panel"
    let settingsTextSize = "Ukuran teks"
    let settingsPaneFont = "Font panel"
    let settingsBlur = "Buram di belakang"
    let settingsNewestFirst = "Terbaru dulu"
    let settingsEngine = "Pengenal suara"
    let settingsSettle = "Jeda mengakhiri kalimat"
    let settingsStop = "Sunyi mengakhiri sesi"
    let settingsAuto = "Otomatis"
    let settingsTranscript = "Transkrip"
    let settingsTerminal = "Terminal"
    let settingsOff = "Mati"
    let settingsHooks = "Hook Claude Code"
    let settingsHooksHint = "Setelah dipasang, Claude Code memberi tahu saat sebuah giliran dimulai, selesai, atau menunggu jawaban — bukan menunggu Clawdline melihatnya pada pemeriksaan berikutnya. Semuanya tetap dibaca dari layar; ini hanya menentukan secepat apa."
    let settingsHooksInstall = "Pasang"
    let settingsHooksRemove = "Lepas"
    let settingsHooksOff = "Belum dipasang — semua dibaca dari layar"
    let settingsHooksOn = "Terpasang — belum ada sesi yang melapor"
    let settingsHooksLive = "Terpasang, dan sesi sedang melapor"
    let settingsRemote = "Akses jarak jauh"
    let settingsRemoteServe = "Jawab lewat HTTP"
    let settingsRemoteHint = "Menyajikan daftar sesi di 127.0.0.1 supaya bisa dibaca peramban, ponsel lewat terowongan, atau skrip. Mati sampai kamu menyalakannya: satu soket yang mendengarkan menyerahkan nama repositori, branch, dan judul tugas."
    let settingsRemoteDevices = "Perangkat berpasangan"
    let settingsRemoteNoDevices = "Belum ada — tidak ada apa pun di luar Mac ini yang bisa membaca apa pun"
    let settingsRemoteRevokeAll = "Putuskan semua"
    let settingsRemoteOpen = "Buka di peramban"
    let pairingIgnore = "Abaikan"
    func pairingAsks(_ device: String) -> String { "\(device) ingin berpasangan dengan Mac ini" }
    func pairingCode(_ code: String) -> String {
        """
        Ketik kode ini di perangkat itu:

        \(code)

        Berlaku dua menit. Kalau bukan kamu yang barusan meminta, abaikan saja — tanpa kode \
        ini yang meminta tidak bisa menyelesaikannya.
        """
    }
    let settingsTunnel = "Bisa dijangkau dari luar"
    let settingsTunnelQuick = "Alamat buatan otomatis"
    let settingsTunnelNamed = "Domain milikku"
    let settingsTunnelHostname = "Nama host"
    let settingsTunnelHint = "Membuka koneksi keluar lewat cloudflared — tanpa penerusan port, tidak ada yang mendengarkan di jaringanmu. Tidak akan mulai sebelum ada satu perangkat berpasangan, karena di balik terowongan itu ada setiap nama repositori dan setiap judul tugas di Mac ini."
    let settingsRemoteWrite = "Biarkan perangkat berpasangan mengetik"
    let settingsRemoteWriteHint = "Mati, perangkat berpasangan hanya bisa membaca. Menyala, ia bisa mengirim teks ke dalam sesi dan memulai sesi baru — dan itu menjalankan kode di Mac ini, karena memang itu yang dikerjakan Claude Code. Ini keputusan yang lain dari yang di atas, jadi sakelarnya juga lain."
    let settingsOpenFile = "Buka berkas konfigurasi…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f dtk", value) }

    let menuOpen = "Buka bilah masukan"
    let menuReveal = "Ke tab tujuan"
    let menuMascot = "Maskot"
    let menuLogin = "Jalankan saat masuk"
    let menuEditConfig = "Pengaturan…"
    let menuReload = "Muat ulang konfigurasi"
    let menuQuit = "Keluar dari Clawdline"
    let menuNoTarget = "(belum terdeteksi)"

    func hotkeyFailedTitle(_ combo: String) -> String { "Tidak bisa mendaftarkan \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Kemungkinan besar sudah dipakai aplikasi lain — Spotlight, pengganti metode \
        masukan, BetterTouchTool, dan sejenisnya.

        Pilih yang lain: ubah "hotkey" di \(configPath), lalu pilih \
        "Muat ulang konfigurasi" dari bilah menu.

        Sementara itu, ✳ di bilah menu tetap membuka bilah masukan.
        """
    }
    let loginFailed = "Tidak bisa mengatur jalan saat masuk"
}
