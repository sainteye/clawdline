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

    let menuOpen = "Buka bilah masukan"
    let menuReveal = "Ke tab tujuan"
    let menuMascot = "Maskot"
    let menuLogin = "Jalankan saat masuk"
    let menuEditConfig = "Ubah konfigurasi…"
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
