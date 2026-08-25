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
        // `waitUntilExit` is fine here: this thread's run loop has nothing of ours on it, so
        // polling it turns nobody's timer and delivers nobody's block.
        DispatchQueue.global(qos: .userInitiated).async {
            self.waitUntilExit()
            exited.signal()
        }
        exited.wait()
    }
}
