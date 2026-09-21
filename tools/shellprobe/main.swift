// A macOS shell with nothing in it but the daemon's lifecycle, for
// tools/prove-shell-stops-daemon.sh.
//
// It is compiled with shell/darwin/Daemon.swift — the file the real shell
// uses, not a copy — so what the script proves about stopping the daemon is
// proved about the shell. No window, no menu bar item, no hotkey: it can run
// beside a person's own Clawdline Next without touching anything of theirs.
//
// `--legacy` is the shell as it was until 2026-09-21: SIGTERM sent to the
// daemon on the way out and not waited for, and SIGTERM to the shell itself
// left to its default.
import AppKit

func shellLog(_ line: String) {
    print(line)
    fflush(stdout)
}

final class Probe: NSObject, NSApplicationDelegate {
    let daemon = BundledDaemon()
    let legacy = CommandLine.arguments.contains("--legacy")
    var termination: DispatchSourceSignal?

    func applicationDidFinishLaunching(_ note: Notification) {
        NSApp.setActivationPolicy(.accessory)
        if !legacy {
            termination = quitOnSIGTERM { NSApp.terminate(nil) }
        }
        let env = ProcessInfo.processInfo.environment
        guard let binary = env["PROBE_DAEMON"] else {
            shellLog("probe: PROBE_DAEMON is not set")
            exit(2)
        }
        daemon.start(binary: URL(fileURLWithPath: binary), environment: env)
    }

    func applicationWillTerminate(_ note: Notification) {
        if legacy {
            daemon.process?.terminate()
            shellLog("probe: quit: SIGTERM sent to the daemon, not waited for")
            return
        }
        shellLog("probe: quit: daemon \(daemon.stop())")
    }
}

let app = NSApplication.shared
let probe = Probe()
app.delegate = probe
app.run()
