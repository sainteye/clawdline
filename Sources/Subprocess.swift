import Foundation

/// Waiting for a subprocess without letting the rest of the app run underneath you.
///
/// `Process.waitUntilExit()` does not simply block: it **polls the current run loop** while it
/// waits. On a background thread that costs nothing, because there is nothing scheduled on that
/// thread's run loop. On the main thread it means every timer, every `DispatchQueue.main.async`
/// and every observer fires *inside* the wait — so a function that shells out is a function that
/// can be re-entered halfway through, at a point its author never had to think about.
///
/// That is not a theory. `Orchestrator.beat` walks the live tasks on the main thread, types into a
/// terminal through `osascript` on the way, and was found overlapping with itself: a second walk
/// started inside the first one's subprocess wait, holding a copy of a task the first walk was
/// about to advance. Measured, a one-second `waitUntilExit()` on the main thread let a
/// two-hundred-millisecond timer fire five times; the same wait through here lets it fire none.
///
/// The fix is to do the waiting somewhere a run loop turning costs nothing, and to block here on
/// something that has no opinion about run loops at all.
extension Process {
    /// Block until this process exits, running nothing else on this thread.
    ///
    /// Interchangeable with `waitUntilExit()` — same duration, and the process is reaped the same
    /// way — except that whatever called it stays the only thing on the stack.
    func waitQuietly() {
        guard isRunning else { return }
        let exited = DispatchSemaphore(value: 0)
        // A thread of its own rather than `DispatchQueue.global`, and that is not a preference.
        // The caller is usually already on a global queue, and blocking one of that pool's threads
        // to wait for another of the same pool is a deadlock as soon as the pool is full: the
        // waiter holds a thread the waited-for block needs. It is not hypothetical — this shipped
        // that way for an afternoon and stopped every reading in the app, because the one place
        // that reads every terminal runs on that pool and shells out from inside it.
        //
        // `waitUntilExit` is still what does the waiting, and it is fine here: a thread made for
        // this has nothing of ours on its run loop, so polling it turns nobody's timer and
        // delivers nobody's block.
        let waiter = Thread {
            self.waitUntilExit()
            exited.signal()
        }
        waiter.stackSize = 64 * 1024
        waiter.start()
        exited.wait()
    }

    /// Run one program without a shell and collect its standard output, with standard error kept
    /// apart and a hard ceiling on how long it may take. `nil` when it could not be started.
    ///
    /// For callers that parse what a program prints — `tar -t`, `lsof -F` — and so must never see
    /// its diagnostics mixed into the records. Both pipes are drained at once, or a chatty stderr
    /// fills its buffer while stdout is being read and the program never exits.
    static func collect(_ executable: String, _ arguments: [String],
                        environment: [String: String]? = nil,
                        timeout: TimeInterval) -> (status: Int32, output: Data)? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        if let environment { process.environment = environment }
        let output = Pipe()
        let errors = Pipe()
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = output
        process.standardError = errors
        do { try process.run() } catch { return nil }
        let killer = DispatchWorkItem { if process.isRunning { process.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: killer)
        let drained = DispatchGroup()
        drained.enter()
        DispatchQueue.global(qos: .utility).async {
            _ = errors.fileHandleForReading.readDataToEndOfFile()
            drained.leave()
        }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        drained.wait()
        process.waitQuietly()
        killer.cancel()
        return (process.terminationStatus, data)
    }
}
