import AppKit

/// Settings and history, kept in ~/.config/clawdline/.
/// Everything has a default: a missing, corrupt or half-written config must still launch.
final class Config {
    static let shared = Config()

    var yFraction: CGFloat = 0.30      // where the panel top sits, as a fraction of screen height
    var width: CGFloat = 720
    var hotKey = "option+space"
    /// The hotkey only fires while one of these apps is frontmost. Comma-separated bundle
    /// ids; empty string means global. More than one matters now that tmux lets Claude Code
    /// run under any terminal — an iTerm2-only scope would leave those users without a key.
    /// Done in-app rather than handed to a hotkey utility: registering globally takes ⌥Space away
    /// from every other app, while registering per-frontmost leaves them exactly as they were.
    var scopeApp = "com.googlecode.iterm2"
    /// "auto" follows the system, or a tag such as "en" / "zh-Hant"
    var language = "auto"
    /// Which mascot pack to draw. Files live in ~/.config/clawdline/mascots/<name>.json
    var mascot = "clawd"
    /// Where tmux lives. Apps do not inherit a login shell, so PATH almost never has it.
    /// Empty means "look in the usual places".
    /// How tall the output pane is, in points.
    var outputHeight: CGFloat = 340
    /// The font the ⌘J pane draws with. Match it to your terminal's, or the box-drawing
    /// characters a status line is made of come out at the wrong widths.
    /// What ⌘J shows: "auto" prefers the transcript and falls back to the terminal,
    /// "transcript" or "terminal" pin it.
    var outputMode = "auto"
    var outputFont = "Menlo"
    /// Point size of the ⌘J pane, adjustable live with ⌘+ / ⌘-.
    var outputSize: CGFloat = 11.5
    /// Newest message at the top instead of the bottom, toggled live with ⌘R.
    /// Only the transcript reads this way: a terminal capture is a picture of a grid, and
    /// flipping its lines would have a wrapped sentence reading upwards.
    var outputNewestFirst = false
    /// Which recogniser the microphone uses.
    ///
    /// "apple" is live and needs nothing installed. "whisper" transcribes when you stop and
    /// handles a sentence with two languages in it, at the cost of a binary and a model file —
    /// see docs/whisper.md. "auto" uses whisper when both are present.
    var voiceEngine = "auto"
    var whisperBinary = ""
    var whisperModel = ""
    /// What language to transcribe in: a BCP-47 tag like "zh-TW" or "en", or "auto".
    ///
    /// "auto" lets Whisper decide, which is what you want when you really do switch languages —
    /// and what you do not want in a quiet room, because a model asked to identify silence will
    /// pick something. Naming a language also fixes the script: Whisper writes Simplified for
    /// Chinese unless told otherwise.
    var voiceLanguage = "auto"
    /// How long a pause ends a sentence, in seconds. 0 turns it off.
    ///
    /// At a pause the words so far are fixed: Whisper reads that stretch and replaces it, and
    /// nothing after that point rewrites it. Without this, a two-minute dictation is one lump
    /// that gets re-transcribed at the end — slower, and everything you already read moves.
    var voiceSettleSeconds: Double = 1.8
    /// How long a silence ends the whole session, in seconds. 0 leaves the microphone on until
    /// it is pressed again.
    ///
    /// Longer than a settle, because these are different claims: a pause says "that sentence is
    /// finished", a long one says "I am finished". Being late costs an open microphone in a room
    /// where nobody is talking; being early costs the keystroke this exists to remove.
    var voiceStopSeconds: Double = 4.0
    /// Words a transcriber cannot be expected to know, put back afterwards.
    ///
    /// Names, product names, the odd piece of jargon — anything that comes back as something
    /// that merely sounds right ("cloud code"). Apple's recogniser is told to expect them;
    /// Whisper is not, because its only lever is a writing sample and a list of words in one
    /// costs all the punctuation. So they are repaired in the text instead, which is
    /// deterministic and cannot make the sentence worse.
    var voiceVocabulary: [String] = []
    /// Hand images over as images rather than as paths.
    ///
    /// Claude Code turns an image on the system pasteboard into `[Image #3]` when it receives a
    /// Ctrl-V, which is a keystroke rather than text — so the send is split around it and the
    /// pasteboard is borrowed and handed back. What you get is the picture in the message and a
    /// number you can point at, instead of forty characters of directory.
    ///
    /// Set false to go back to sending the path, which is never wrong, only plainer. It also
    /// falls back on its own for anything that is not a Claude Code session, because Ctrl-V in a
    /// shell means something else entirely.
    var sendImagesAsPaste = true
    /// Come back when the terminal does.
    ///
    /// Switching away from a panel you left open is "I need to see something for a moment", not
    /// "I am done with it" — that is what Esc is for. So what an app switch put away, coming
    /// back takes out again. Turn it off if you would rather every appearance be one you asked
    /// for by hand.
    var reopenOnReturn = true
    /// The character that lives in the notch, and whether it lives there.
    ///
    /// **This one is an experiment and is meant to be switchable off in one word.** It tells you
    /// nothing the menu bar mark does not; it is the same reading wearing a costume, and whether
    /// a small animal leaning out of your camera housing is a delight or an irritation is not a
    /// question anybody can answer on your behalf. `false` and it is not created at all — no
    /// window, no observer, no drawing.
    var notch = true
    /// Move the terminal's own tab to whatever the bar is pointing at.
    ///
    /// The bar names its target along the bottom edge and that has always been enough to send
    /// safely — but the target and the tab in front of you were free to be two different
    /// sessions, and the moment you closed the panel you were looking at the wrong one. With this
    /// on they are the same session by construction.
    ///
    /// **Selecting is not the same as activating**, and only the first one happens: iTerm2 is not
    /// brought forward, or every press of Tab would take the keyboard out of the box you are
    /// typing into. Off for anyone who keeps a terminal tab open to read while working elsewhere.
    var followTarget = true
    /// How solid the card is, from 0 (pure frosted glass) to 1 (opaque).
    ///
    /// The material samples whatever is behind the window, so a screen of green diff or a
    /// bright page tints the whole card and drags the text with it. This is a dark layer
    /// between the two: the blur still reads as glass, but the colour behind stops arriving.
    var cardOpacity: Double = 0.55
    /// How far the ⌘J backdrop goes, from 0 (none) to 1 (fully obscured).
    /// Below 1 the blur is partly transparent, so what is behind stays legible.
    var backdropStrength: Double = 0.5
    /// Believe what Claude Code's hooks say about a session, when they are installed.
    ///
    /// Off, and the notes are ignored while the hooks stay wired up — which is the setting to
    /// reach for if a reading ever looks wrong and you want to know whether this is why, without
    /// editing another program's settings file to find out. Nothing else changes: the screen is
    /// still the complete fallback when no matched lifecycle note states more. See
    /// Sources/HookBridge.swift.
    var hooks = true
    /// Believe what Claude Code's session registry says a session is doing.
    ///
    /// Each session writes a small file about itself under `~/.claude/sessions/` and keeps the
    /// status in it current, so unlike a hook this is there whether or not anybody installed
    /// anything — including for sessions that were already open. Off, and the files are ignored
    /// and every reading is the screen's alone, which is the setting to reach for if a state ever
    /// looks wrong and you want to know whether this is why. See Sources/SessionRegistry.swift.
    var sessionRegistry = true
    /// Answer questions over HTTP, on the loopback address, so that a browser or a script can ask
    /// what the panel asks.
    ///
    /// **Off, and that is not a shy default.** A listening socket is the difference between a
    /// program on your machine and a service on your machine, and reading a session hands over a
    /// repository name, a branch and a task title. Turning it on should be a thing somebody did
    /// on purpose. See docs/remote.md and Sources/RemoteServer.swift.
    var remote = false
    var remotePort = 7717
    /// How the outside gets in: "off", "quick" (a generated trycloudflare.com name, no account),
    /// or "named" (your own tunnel and your own hostname). See Sources/RemoteTunnel.swift.
    /// Let a paired device type into a session, and start new ones.
    ///
    /// **Separate from `remote`, and off, because it is a different feature at a different risk
    /// level.** Reading a session discloses a repository name and a task title. Writing to one is
    /// remote code execution — Claude Code runs `bash` — so this is not a finer setting on the
    /// same dial, it is the second dial. Turning it on grants `send` to every paired device;
    /// turning it off takes it back from all of them at once.
    /// A command to run whenever a session changes state — argv, not a shell line.
    ///
    /// The one extension point that pays for itself before anything else does. In Herdr's
    /// ecosystem, where 682 plugins were counted, the single event "this agent's state changed"
    /// accounts for 15% of every hook declared and appears in 44% of the plugins that hook
    /// anything at all: notifications, status lines, dashboards, watchdogs and chat bridges are
    /// all the same shape. So it is the first thing here to be opened up, and it is opened the
    /// way they opened theirs — environment variables and an executable, no SDK, no bindings, no
    /// opinion about what language you write it in.
    ///
    /// `["node", "~/bin/notify.mjs"]`. An array because nothing should be word-split: a path with
    /// a space in it is a path, not two arguments. See Sources/StateHook.swift.
    /// Buzz when a turn that took a while finishes, not when any turn finishes.
    ///
    /// **On by default, and the threshold underneath is what makes that defensible.** A turn ends
    /// dozens of times a day and the unthresholded version would be unusable; over two minutes it
    /// is a different event, because two minutes is roughly where somebody stops watching and goes
    /// to do something else. See `StateHook.finishThreshold`.
    ///
    /// This was off when it shipped, on the argument that a notification you already knew about
    /// trains you to ignore the ones you did not. The owner's call on 2026-08-19 was the other
    /// way, and the reasoning is worth keeping: at this volume a notification arriving is itself
    /// useful confirmation the thing works, and **a rule elaborate enough to suppress the
    /// redundant ones is a rule nobody can debug when it goes quiet**. A feature that is off by
    /// default is also a feature most people never find.
    var pushOnFinish = true
    /// Buzz when a deploy stops running.
    ///
    /// A better candidate than the one above and for the opposite reason: deploys are rare, and
    /// you are usually waiting on one rather than merely interested. Both outcomes are sent —
    /// a deploy that failed is the one you most want to hear about.
    var pushOnDeploy = false
    var onStateChange: [String] = []
    var remoteWrite = false
    var remoteTunnel = "off"
    var remoteTunnelName = ""
    var remoteHostname = ""
    var cloudflaredPath = ""
    var tmuxPath = ""
    /// Where Codex keeps its sessions, when it is not `~/.codex`.
    ///
    /// Codex honours `CODEX_HOME`, and this app cannot see it: launched from Finder it inherits
    /// no login shell, which is the same reason ``Tmux/binary`` cannot look on `PATH`. Blank
    /// means the environment if it has one and `~/.codex` otherwise; `~` is expanded.
    var codexHome = ""
    /// Give a new Codex conversation a short user-facing name from its first request.
    ///
    /// Off until somebody chooses it because this is not local bookkeeping: it starts one small
    /// Codex turn, sends that request to the configured model and spends Codex usage. The helper
    /// turn is ephemeral, so the act of naming a session never creates another session to name.
    var codexAutoName = false
    /// The deliberately small model used for that one narrow turn. Kept configurable because
    /// model availability belongs to the account, not to this binary; the default is the current
    /// low-cost Codex model documented for clear, repeatable work.
    var codexAutoNameModel = "gpt-5.6-luna"
    /// An escape hatch for a Finder-launched app whose running Codex process cannot yield its
    /// executable path. Blank means use that process first, then the usual install locations.
    var codexPath = ""
    /// Let a root session ask this app to open child sessions and brief them. See
    /// Sources/Orchestrator.swift and docs/orchestrator.md.
    ///
    /// On by default, and that is defensible where `remote` being off is not: dispatching already
    /// requires a credential that only a local process can read — the 0600 token file — so this
    /// switch is a preference, not the boundary. Off refuses dispatch outright while leaving the
    /// task records readable.
    var orchestratorEnabled = true
    /// How many child sessions one root may have alive at once.
    ///
    /// Per dispatcher rather than per Mac, because a child may now dispatch in turn and the two
    /// levels are not the same appetite: five errands from the session a person is sitting in,
    /// three from a session that is itself an errand. `orchestratorMaxGrandchildren` is the
    /// second number, and `orchestratorMaxDescendants` is the ceiling over both.
    var orchestratorMaxChildren = 5
    /// How many child sessions a *child* may have alive at once. `0` forbids it outright, which
    /// is the old behaviour: depth stops at one and a child that tries is refused at the door.
    ///
    /// Smaller than the root's number on purpose. A child was handed one job by a session that
    /// was handed nothing; the further from the person at the keyboard a decision is made, the
    /// less of this Mac it should be able to spend on it.
    var orchestratorMaxGrandchildren = 3
    /// How far a dispatched child may go before it stops and asks — the *most* a task may ask
    /// for, not what every task gets. `ask`, `auto`, `full`; see ``Permission``.
    ///
    /// `auto` by default, and that is a considered position rather than a convenience. Nobody is
    /// watching a child's tab: a session that stops for approval stops until it times out, so
    /// "ask about everything" is not the safe setting here, it is the one where the work silently
    /// does not happen. What `auto` keeps is the assistant's own judgement about what a person
    /// would want to be asked, which is the same judgement it uses in a session somebody *is*
    /// watching.
    ///
    /// `full` is not reachable without setting it here. A task can ask for it and be quietly
    /// given `auto` instead, because the session doing the asking is not the one that lives with
    /// what happens next — the person at this Mac is, and this is where they answer.
    var orchestratorPermission = "auto"
    /// Every dispatched session on this Mac, whoever asked. One full tree's worth — a root's
    /// children and each of their children — and not a setting of its own, because it is not a
    /// choice anybody makes separately from the two numbers it is made of.
    ///
    /// The per-dispatcher caps say what one session may spend. This says what the machine may
    /// spend, and it is the half that still holds when a caller lies about who it is: declaring
    /// somebody else's session id moves a task into another bucket, never past this line.
    /// The ceiling as the type, with the file's word for it read back through the closed list so
    /// a hand-edit that says something else lands on the default rather than on nothing.
    var orchestratorPermissionCeiling: Permission {
        Permission(rawValue: orchestratorPermission) ?? .auto
    }
    var orchestratorMaxDescendants: Int {
        orchestratorMaxChildren * (1 + orchestratorMaxGrandchildren)
    }
    /// Type one line into the root session when a task it dispatched finishes, so the
    /// conversation that asked for the work is the one that hears it is done.
    var orchestratorNotifyRoot = true
    /// What becomes of a child's terminal once it has reported: seconds to leave it open before
    /// the app closes it, `0` to close as soon as it is quiet, `-1` to leave it to the user.
    /// Only a child that reported — success or failure — is closed; one that timed out or never
    /// came up is left where it is, because what went wrong is on that screen.
    var orchestratorChildLinger = 180
    /// Where the project status files are read from, and where the icon registry lives.
    ///
    /// Both default to what claude-bestiary writes, because that is what most people reading this
    /// already have — but the format is documented (docs/project-status.md) so that anything can
    /// produce them, and a producer that is not claude-bestiary should not have to impersonate it
    /// to be found. Blank means the default; `~` is expanded.
    var statusDir = ""
    var iconsFile = ""
    var lastTargetID: String?
    var history: [String] = []

    /// True when at least one device has been paired or a password set.
    ///
    /// Read by ``RemoteTunnel`` before it will start anything: **a tunnel to an endpoint with no
    /// authentication is a mistake that should not be reachable by editing one config key**, and
    /// what is behind it is a list of your repositories, branches and task titles.
    var remoteAuthConfigured: Bool { RemoteAuth.isConfigured }

    private let dir = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent(".config/clawdline", isDirectory: true)
    private var file: URL { dir.appendingPathComponent("config.json") }

    private init() { load() }

    func load() {
        guard let data = try? Data(contentsOf: file),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        if let v = obj["y_fraction"] as? Double, v > 0.02, v < 0.9 { yFraction = CGFloat(v) }
        if let v = obj["width"] as? Double, v >= 360, v <= 1400 { width = CGFloat(v) }
        if let v = obj["hotkey"] as? String, !v.isEmpty { hotKey = v }
        if let v = obj["scope_app"] as? String { scopeApp = v }
        if let v = obj["language"] as? String, !v.isEmpty { language = v }
        if let v = obj["mascot"] as? String, !v.isEmpty { mascot = v }
        if let v = obj["hooks"] as? Bool { hooks = v }
        if let v = obj["session_registry"] as? Bool { sessionRegistry = v }
        if let v = obj["remote"] as? Bool { remote = v }
        if let v = obj["remote_port"] as? Int, v > 0, v < 65536 { remotePort = v }
        if let v = obj["push_on_finish"] as? Bool { pushOnFinish = v }
        if let v = obj["push_on_deploy"] as? Bool { pushOnDeploy = v }
        if let v = obj["on_state_change"] as? [String] { onStateChange = v }
        if let v = obj["remote_write"] as? Bool { remoteWrite = v }
        if let v = obj["remote_tunnel"] as? String, !v.isEmpty { remoteTunnel = v }
        if let v = obj["remote_tunnel_name"] as? String { remoteTunnelName = v }
        if let v = obj["remote_hostname"] as? String { remoteHostname = v }
        if let v = obj["cloudflared_path"] as? String { cloudflaredPath = v }
        if let v = obj["tmux_path"] as? String { tmuxPath = v }
        if let v = obj["codex_home"] as? String { codexHome = v }
        if let v = obj["codex_auto_name"] as? Bool { codexAutoName = v }
        if let v = obj["codex_auto_name_model"] as? String, !v.isEmpty {
            codexAutoNameModel = v
        }
        if let v = obj["codex_path"] as? String { codexPath = v }
        if let v = obj["orchestrator_enabled"] as? Bool { orchestratorEnabled = v }
        if let v = obj["orchestrator_max_children"] as? Int, v >= 1, v <= 10 {
            orchestratorMaxChildren = v
        }
        if let v = obj["orchestrator_max_grandchildren"] as? Int, v >= 0, v <= 10 {
            orchestratorMaxGrandchildren = v
        }
        if let v = obj["orchestrator_permission"] as? String,
           Permission(rawValue: v) != nil {
            orchestratorPermission = v
        }
        if let v = obj["orchestrator_notify_root"] as? Bool { orchestratorNotifyRoot = v }
        if let v = obj["orchestrator_child_linger"] as? Int, v >= -1, v <= 3600 {
            orchestratorChildLinger = v
        }
        if let v = obj["status_dir"] as? String { statusDir = v }
        if let v = obj["icons_file"] as? String { iconsFile = v }
        if let v = obj["output_height"] as? Double, v >= 80, v <= 900 { outputHeight = CGFloat(v) }
        if let v = obj["backdrop"] as? Double, v >= 0, v <= 1 { backdropStrength = v }
        if let v = obj["output_font"] as? String, !v.isEmpty { outputFont = v }
        if let v = obj["output_mode"] as? String, !v.isEmpty { outputMode = v }
        if let v = obj["output_size"] as? Double, v >= 8, v <= 28 { outputSize = CGFloat(v) }
        if let v = obj["output_newest_first"] as? Bool { outputNewestFirst = v }
        if let v = obj["card_opacity"] as? Double, v >= 0, v <= 1 { cardOpacity = v }
        if let v = obj["reopen_on_return"] as? Bool { reopenOnReturn = v }
        if let v = obj["notch"] as? Bool { notch = v }
        if let v = obj["follow_target"] as? Bool { followTarget = v }
        if let v = obj["voice_engine"] as? String, !v.isEmpty { voiceEngine = v }
        if let v = obj["voice_settle_seconds"] as? Double, v >= 0, v <= 30 { voiceSettleSeconds = v }
        if let v = obj["voice_stop_seconds"] as? Double, v >= 0, v <= 300 { voiceStopSeconds = v }
        if let v = obj["voice_vocabulary"] as? [String] { voiceVocabulary = v }
        if let v = obj["send_images_as_paste"] as? Bool { sendImagesAsPaste = v }
        if let v = obj["voice_language"] as? String, !v.isEmpty { voiceLanguage = v }
        if let v = obj["whisper_binary"] as? String { whisperBinary = v }
        if let v = obj["whisper_model"] as? String { whisperModel = v }
        if let v = obj["last_target_id"] as? String { lastTargetID = v }
        if let v = obj["history"] as? [String] { history = v }
        // What the file said, so a later save can tell an edit of ours from an edit of theirs.
        known = obj
    }

    /// Everything this object holds, as it would be written.
    private var serialised: [String: Any] {
        var obj: [String: Any] = [
            "y_fraction": Double(yFraction),
            "width": Double(width),
            "hotkey": hotKey,
            "scope_app": scopeApp,
            "language": language,
            "mascot": mascot,
            "hooks": hooks,
            "session_registry": sessionRegistry,
            "remote": remote,
            "remote_port": remotePort,
            "push_on_finish": pushOnFinish,
            "push_on_deploy": pushOnDeploy,
            "on_state_change": onStateChange,
            "remote_write": remoteWrite,
            "remote_tunnel": remoteTunnel,
            "remote_tunnel_name": remoteTunnelName,
            "remote_hostname": remoteHostname,
            "cloudflared_path": cloudflaredPath,
            "tmux_path": tmuxPath,
            "codex_home": codexHome,
            "codex_auto_name": codexAutoName,
            "codex_auto_name_model": codexAutoNameModel,
            "codex_path": codexPath,
            "orchestrator_enabled": orchestratorEnabled,
            "orchestrator_max_children": orchestratorMaxChildren,
            "orchestrator_max_grandchildren": orchestratorMaxGrandchildren,
            "orchestrator_permission": orchestratorPermission,
            "orchestrator_notify_root": orchestratorNotifyRoot,
            "orchestrator_child_linger": orchestratorChildLinger,
            "status_dir": statusDir,
            "icons_file": iconsFile,
            "output_height": Double(outputHeight),
            "backdrop": backdropStrength,
            "output_font": outputFont,
            "output_mode": outputMode,
            "output_size": Double(outputSize),
            "output_newest_first": outputNewestFirst,
            "card_opacity": cardOpacity,
            "reopen_on_return": reopenOnReturn,
            "notch": notch,
            "follow_target": followTarget,
            "voice_engine": voiceEngine,
            "voice_settle_seconds": voiceSettleSeconds,
            "voice_stop_seconds": voiceStopSeconds,
            "voice_vocabulary": voiceVocabulary,
            "send_images_as_paste": sendImagesAsPaste,
            "voice_language": voiceLanguage,
            "whisper_binary": whisperBinary,
            "whisper_model": whisperModel,
            "history": Array(history.suffix(60)),
        ]
        // Only when there is one. A Swift Optional in this dictionary is not a JSON value, and
        // JSONSerialization throws on it — which `try?` then swallowed, so on a fresh install
        // nothing was saved at all until a target had been picked.
        if let id = lastTargetID { obj["last_target_id"] = id }
        return obj
    }

    /// What was on disk when we last read or wrote it. The reference point for "did we change
    /// this, or did they?"
    private var known: [String: Any] = [:]

    /// Keep what somebody edited by hand, keep what the app changed, and do not make them race.
    ///
    /// The app rewrites this file whenever anything moves — a prompt goes into the history, ⌘+
    /// changes the text size — and it used to write everything it held in memory. So editing
    /// the file while the app was running lost the edit, silently and at an unpredictable
    /// moment. "Quit first" was the documented answer, which is another way of saying the
    /// feature did not work.
    ///
    /// A key the app has not touched since it read the file is the file's to answer. A key the
    /// app has changed is the app's. Keys it does not know about are passed through untouched,
    /// so a setting from a newer version survives being opened by an older one.
    ///
    /// Both changed the same key: the app wins. It changed it because somebody pressed
    /// something, and that is the more recent of the two intentions we can see.
    static func merged(mine: [String: Any], known: [String: Any],
                       onDisk: [String: Any]) -> [String: Any] {
        var out = onDisk
        for (key, value) in mine where !equal(value, known[key]) { out[key] = value }
        for (key, value) in mine where out[key] == nil { out[key] = value }
        return out
    }

    private static func equal(_ a: Any?, _ b: Any?) -> Bool {
        switch (a, b) {
        case (nil, nil): return true
        case (nil, _), (_, nil): return false
        default: return (a as? NSObject)?.isEqual(b as? NSObject) ?? false
        }
    }

    func save() {
        let mine = serialised
        let onDisk = (try? Data(contentsOf: file))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        let obj = Self.merged(mine: mine, known: known, onDisk: onDisk)

        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys])
        else {
            Log.write("config: could not serialise, nothing written")
            return
        }
        do {
            try data.write(to: file)
            known = obj
        } catch {
            // Worth a line: everything above this is best-effort, and a config that silently
            // stops persisting looks exactly like one that is being ignored.
            Log.write("config: could not write — \(error.localizedDescription)")
        }
    }

    var fileURL: URL { file }
}
