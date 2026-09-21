// The daemon this shell starts, and the only one it ever stops.
//
// The shell starts the daemon inside its own bundle and stops it when the
// shell quits — by the pid it was handed at launch, never by name. A daemon
// somebody started any other way (`clawdline serve` in a terminal, a launchd
// job, a systemd unit elsewhere) is not this shell's, survives it, and is how
// a person keeps a daemon running without the window. That is the whole answer
// to "should quitting the app stop the daemon": the one it started, yes, and
// provably; any other, never.
//
// Nothing here kills a process it cannot name. A daemon left over from a shell
// that crashed is not guessed at by the next one: the next one's daemon finds
// the port held and says by whom in daemon.log (cmd/clawdline/portheld.go),
// and exits with `exitPortHeld`, which is logged here as that.
import Foundation

final class BundledDaemon {
    /// The status `clawdline serve` exits with when another process holds its
    /// port (cmd/clawdline/portheld.go `exitPortHeld`).
    static let exitPortHeld: Int32 = 3

    private(set) var process: Process?

    /// Start `binary serve` with `environment`. The child is put in a process
    /// group of its own by Foundation, which is what lets `stop` reach the
    /// processes it started as well when it has to force it.
    @discardableResult
    func start(binary: URL, environment: [String: String]) -> Bool {
        let task = Process()
        task.executableURL = binary
        task.arguments = ["serve"]
        task.environment = environment
        task.terminationHandler = { ended in
            let pid = ended.processIdentifier
            if ended.terminationReason == .exit && ended.terminationStatus == Self.exitPortHeld {
                shellLog("daemon: pid \(pid) exited because another process holds the port;"
                         + " daemon.log names it, and this window shows whatever answers there")
            } else if ended.terminationReason == .uncaughtSignal {
                shellLog("daemon: pid \(pid) ended on signal \(ended.terminationStatus)")
            } else {
                shellLog("daemon: pid \(pid) exited with status \(ended.terminationStatus)")
            }
        }
        do {
            try task.run()
        } catch {
            shellLog("daemon: could not start: \(error.localizedDescription)")
            return false
        }
        process = task
        shellLog("daemon: started pid \(task.processIdentifier) from the bundle")
        return true
    }

    /// Stop the daemon this shell started and wait until it has gone.
    ///
    /// SIGTERM to its pid first: the daemon takes its tunnel down on that and
    /// then ends (cmd/clawdline `stopTunnelOnSignal`). Only if it is still
    /// there after `grace` is it forced, and then its whole process group is,
    /// so that a cloudflared it started does not outlive it — but only when
    /// that group is the daemon's own, never this shell's. The answer is
    /// returned for the log, and says which of those happened.
    @discardableResult
    func stop(grace: TimeInterval = 5) -> String {
        guard let task = process else { return "no daemon was started by this shell" }
        let pid = task.processIdentifier
        guard task.isRunning else { return "pid \(pid) had already ended" }
        task.terminate()
        if waitForExit(task, within: grace) {
            return "pid \(pid) stopped on SIGTERM"
        }
        let group = getpgid(pid)
        if group == pid && group != getpgrp() {
            kill(-group, SIGKILL)
        } else {
            kill(pid, SIGKILL)
        }
        if waitForExit(task, within: 2) {
            return "pid \(pid) was still running \(Int(grace))s after SIGTERM and was killed"
        }
        return "pid \(pid) is still running after SIGKILL"
    }

    /// `Process.waitUntilExit` without the wait being unbounded: it turns the
    /// run loop the same way, which is what lets Foundation reap the child and
    /// `isRunning` go false while the main thread is here.
    private func waitForExit(_ task: Process, within seconds: TimeInterval) -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while task.isRunning && Date() < deadline {
            RunLoop.current.run(mode: .default, before: Date().addingTimeInterval(0.05))
        }
        return !task.isRunning
    }
}

/// `kill`, `killall` and launchd's stop all send SIGTERM, whose default is to
/// end the shell on the spot: no applicationWillTerminate, so `stop` never
/// runs and the daemon is left on the port with launchd as its parent. It is
/// taken as a quit instead. Keep the returned source; it stops when released.
func quitOnSIGTERM(_ quit: @escaping () -> Void) -> DispatchSourceSignal {
    signal(SIGTERM, SIG_IGN)
    let source = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
    source.setEventHandler {
        shellLog("quit: SIGTERM")
        quit()
    }
    source.resume()
    return source
}
