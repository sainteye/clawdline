import Foundation

// W2-1 closed nine bare `Orchestrator.lock` regions into `ScheduleService`, six into
// `OrchestratorEventPublisher`, and gave the pre-existing bounded terminal mutation lane its own
// file, `TerminalCommandScheduler`. This suite proves each new owner's own behavior directly
// (rather than only through whatever paths `ScheduledDispatchTests`/`OrchestratorCompletionTests`/
// `SessionLaunchTests` already happened to exercise), and that `Orchestrator`/`RemoteServer` still
// answer through the exact legacy facade names those suites call.

func runW2ApplicationOwnershipTests() {
    group("ScheduleService owns the schedule-beat tables and their limits/order") {
        let id = "w2-owner-\(UUID().uuidString)"
        let filename = "\(id).json"
        let t0 = Date(timeIntervalSince1970: 1_800_000_000)

        // Manual-run admission: refused while active, while already dispatching, and while a
        // fire is already pending — admitted only when none of the three hold.
        check("manual run is refused while a task from this schedule is already active",
              !OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.admitManualRunLocked(id: id, hasActiveScheduleTask: true)
              })
        check("manual run is admitted and marks the schedule dispatching",
              OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.admitManualRunLocked(id: id, hasActiveScheduleTask: false)
              })
        check("a second manual run is refused while the first is still dispatching",
              !OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.admitManualRunLocked(id: id, hasActiveScheduleTask: false)
              })
        let consumed = ScheduleService.settleManualRun(id: id, succeeded: true, fire: t0)
        check("a succeeded settlement returns the fire it consumed", consumed == t0)
        check("settlement marks the occurrence handled",
              ScheduleService.handledFireForTesting(id) == t0)
        check("dispatching cleared, so a fresh manual run is admitted again",
              OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.admitManualRunLocked(id: id, hasActiveScheduleTask: false)
              })
        let notConsumed = ScheduleService.settleManualRun(id: id, succeeded: false, fire: t0)
        check("a failed settlement consumes nothing", notConsumed == nil)

        // Beat decision bookkeeping: run -> pending, active/missed -> handled (+ last-missed only
        // for missed), and the already-decided short-circuit that stops a re-decided occurrence.
        let id2 = "w2-owner-\(UUID().uuidString)"
        let fire = Date(timeIntervalSince1970: 1_800_003_600)
        check("a fresh occurrence is not already decided",
              !OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.alreadyDecidedLocked(scheduleID: id2, fire: fire)
              })
        OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.recordRunDecidedLocked(scheduleID: id2, fire: fire)
        }
        check("a pending occurrence is already decided",
              OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.alreadyDecidedLocked(scheduleID: id2, fire: fire)
              })
        ScheduleService.releasePendingFireIfCurrent(scheduleID: id2, fire: fire)
        check("releasing the pending mark clears the already-decided state",
              !OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.alreadyDecidedLocked(scheduleID: id2, fire: fire)
              })
        OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.recordHandledLocked(scheduleID: id2, fire: fire, missed: true)
        }
        check("a missed occurrence is recorded last-missed",
              OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.lastMissedLocked(id2)
              } == fire)
        let staleFire = fire.addingTimeInterval(60)
        ScheduleService.releasePendingFireIfCurrent(scheduleID: id2, fire: staleFire)
        check("releasing a stale fire does not disturb the recorded last-missed occurrence",
              OrchestratorRegistry.withTaskRecords { _ in ScheduleService.lastMissedTableLocked() }[id2]
                  == fire)

        // Timer-fire admission: refused (and recorded handled) while active/dispatching; admitted
        // and marks dispatching otherwise; always clears the pending mark either way.
        let id3 = "w2-owner-\(UUID().uuidString)"
        let fire3 = Date(timeIntervalSince1970: 1_800_007_200)
        OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.recordRunDecidedLocked(scheduleID: id3, fire: fire3)
        }
        let refused = OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.admitTimerFireLocked(
                scheduleID: id3, hasActiveScheduleTask: true, fire: fire3)
        }
        check("an active task refuses the timer-driven fire", !refused)
        check("the refused fire is recorded handled instead of dispatched",
              ScheduleService.handledFireForTesting(id3) == fire3)
        // `admitTimerFireLocked` clears the pending mark before deciding admission either way;
        // with the occurrence now handled instead, a later beat asking about the same fire finds
        // it already decided rather than still pending.
        check("the occurrence reads as already decided rather than still pending",
              OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.alreadyDecidedLocked(scheduleID: id3, fire: fire3)
              })
        let id4 = "w2-owner-\(UUID().uuidString)"
        let fire4 = Date(timeIntervalSince1970: 1_800_010_800)
        let admitted = OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.admitTimerFireLocked(
                scheduleID: id4, hasActiveScheduleTask: false, fire: fire4)
        }
        check("no active task admits the timer-driven fire and marks it dispatching", admitted)
        check("a manual run cannot join a schedule the timer already admitted",
              !OrchestratorRegistry.withTaskRecords { _ in
                  ScheduleService.admitManualRunLocked(id: id4, hasActiveScheduleTask: false)
              })
        ScheduleService.settleDispatch(scheduleID: id4, fire: fire4, overCapacity: false)
        check("settlement under capacity marks the occurrence handled",
              ScheduleService.handledFireForTesting(id4) == fire4)
        let id5 = "w2-owner-\(UUID().uuidString)"
        let fire5 = Date(timeIntervalSince1970: 1_800_014_400)
        _ = OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.admitTimerFireLocked(
                scheduleID: id5, hasActiveScheduleTask: false, fire: fire5)
        }
        ScheduleService.settleDispatch(scheduleID: id5, fire: fire5, overCapacity: true)
        check("settlement over capacity leaves the occurrence unhandled so a retry can claim it",
              ScheduleService.handledFireForTesting(id5) == nil)

        // Schedule removal forgets every table together.
        OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.recordHandledLocked(scheduleID: id, fire: t0, missed: true)
        }
        ScheduleService.forgetSchedule(id: id, filename: filename)
        check("removal clears the handled occurrence",
              ScheduleService.handledFireForTesting(id) == nil)
        check("removal clears the last-missed occurrence",
              OrchestratorRegistry.withTaskRecords { _ in ScheduleService.lastMissedLocked(id) }
                  == nil)
    }

    group("ScheduleService's beat-overlap counter and inventory fingerprints") {
        // The overlap counter: a second walk that begins before the first ends observes
        // `overlapping == true`; once both end, a fresh walk observes `false` again.
        let firstOverlap = OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.beginBeatLocked()
        }
        check("the first walk finds nothing overlapping it", !firstOverlap)
        let secondOverlap = OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.beginBeatLocked()
        }
        check("a second walk beginning before the first ends observes the overlap", secondOverlap)
        ScheduleService.endBeat()
        ScheduleService.endBeat()
        let thirdOverlap = OrchestratorRegistry.withTaskRecords { _ in
            ScheduleService.beginBeatLocked()
        }
        check("a fresh walk after both ended observes no overlap", !thirdOverlap)
        ScheduleService.endBeat()

        // Inventory fingerprints: a directory-listing failure clears the table; a scan reports
        // only the files whose fingerprint actually changed since the last scan.
        let a = Orchestrator.InvalidSchedule(
            file: "a.json", error: "bad", kind: "schema", fingerprint: "fp1", title: "A",
            projectDir: nil, notifyOnFailure: false)
        let firstScan = ScheduleService.recordInvalidFingerprints([a])
        expect("a file invalid for the first time is reported", firstScan.map(\.file), [a.file])
        let secondScan = ScheduleService.recordInvalidFingerprints([a])
        check("the same fingerprint is not reported again", secondScan.isEmpty)
        let changed = Orchestrator.InvalidSchedule(
            file: "a.json", error: "still bad", kind: "schema", fingerprint: "fp2", title: "A",
            projectDir: nil, notifyOnFailure: false)
        let thirdScan = ScheduleService.recordInvalidFingerprints([changed])
        expect("a changed fingerprint is reported again", thirdScan.map(\.file), [changed.file])
        ScheduleService.clearInvalidFingerprints()
        let afterClear = ScheduleService.recordInvalidFingerprints([a])
        expect("clearing forgets every fingerprint, so the next scan reports fresh",
               afterClear.map(\.file), [a.file])
        ScheduleService.clearInvalidFingerprints()
    }

    group("OrchestratorEventPublisher coalesces the completion pump and cancels stale generations") {
        Orchestrator.forget()
        let first = OrchestratorEventPublisher.admitCompletionPump()
        check("the first admission schedules a pump", first != nil)
        let second = OrchestratorEventPublisher.admitCompletionPump()
        check("a second admission while one is still scheduled coalesces into it", second == nil)
        guard let generation = first else { return }
        check("the scheduled generation is the live one",
              OrchestratorEventPublisher.isLivePumpGeneration(generation))
        OrchestratorEventPublisher.clearPumpScheduledIfLive(generation)
        let third = OrchestratorEventPublisher.admitCompletionPump()
        check("clearing the flag lets a new pump be admitted", third != nil)
        if let third { OrchestratorEventPublisher.clearPumpScheduledIfLive(third) }

        // Generation cancellation: `forget()` (the production cancellation path) advances the
        // generation, so a pump scheduled before it must not clear a flag scheduled after it, and
        // must not read as live.
        let staleGeneration = OrchestratorEventPublisher.admitCompletionPump()
        guard let staleGeneration else {
            check("a pump generation was available to make stale", false)
            return
        }
        Orchestrator.forget()
        check("a generation from before forget() is no longer live",
              !OrchestratorEventPublisher.isLivePumpGeneration(staleGeneration))
        let freshGeneration = OrchestratorEventPublisher.admitCompletionPump()
        check("forget() left a fresh pump admittable under the new generation", freshGeneration != nil)
        OrchestratorEventPublisher.clearPumpScheduledIfLive(staleGeneration)
        let afterStaleClear = OrchestratorEventPublisher.admitCompletionPump()
        check("clearing a stale generation does not un-schedule the live pump — a fresh "
                + "admission still coalesces into it", afterStaleClear == nil)
        if let freshGeneration { OrchestratorEventPublisher.clearPumpScheduledIfLive(freshGeneration) }
    }

    group("OrchestratorEventPublisher's activity-turn clock advances only on a new working/waiting turn") {
        Orchestrator.forget()
        let terminal = "w2-activity-\(UUID().uuidString)"
        expect("an unseen terminal starts at generation zero",
               Orchestrator.activityGeneration(ofTerminal: terminal), 0)
        let firstEntry = OrchestratorRegistry.withTaskRecords { _ in
            OrchestratorEventPublisher.noteActivityLocked(terminalID: terminal, activityClass: "working")
        }
        check("entering working for the first time is a change", firstEntry)
        expect("the turn clock advanced once",
               Orchestrator.activityGeneration(ofTerminal: terminal), 1)
        let sameClass = OrchestratorRegistry.withTaskRecords { _ in
            OrchestratorEventPublisher.noteActivityLocked(terminalID: terminal, activityClass: "working")
        }
        check("staying in the same class is not a change", !sameClass)
        expect("the turn clock does not advance for a repeated class",
               Orchestrator.activityGeneration(ofTerminal: terminal), 1)
        let toIdle = OrchestratorRegistry.withTaskRecords { _ in
            OrchestratorEventPublisher.noteActivityLocked(terminalID: terminal, activityClass: "idle")
        }
        check("leaving to idle is a change", toIdle)
        expect("idle does not advance the turn clock",
               Orchestrator.activityGeneration(ofTerminal: terminal), 1)
        let toWaiting = OrchestratorRegistry.withTaskRecords { _ in
            OrchestratorEventPublisher.noteActivityLocked(terminalID: terminal, activityClass: "waiting")
        }
        check("a new turn beginning at waiting is a change", toWaiting)
        expect("the turn clock advances on the new waiting turn",
               Orchestrator.activityGeneration(ofTerminal: terminal), 2)
    }

    group("TerminalCommandScheduler enforces total depth, per-channel depth and nested reservation") {
        let scheduler = TerminalCommandScheduler(label: "w2-owner-test-\(UUID().uuidString)")
        expect("depth matches the documented bounded lane", TerminalCommandScheduler.depth, 8)
        expect("per-channel depth matches the documented bounded lane",
               TerminalCommandScheduler.channelDepth, 2)

        // Total depth: the lane's worker is one serial queue, so only a blocker actually runs;
        // everything behind it is merely admitted (its counter incremented synchronously at
        // admission, the same way production dispatch relies on) and queued for its turn. Eight
        // distinct channels admit; a ninth is refused; releasing the blocker drains the rest in
        // order. Mirrors the working pattern in `SessionLaunchTests.swift`'s broker-depth group.
        let blockerEntered = DispatchSemaphore(value: 0)
        let blockerRelease = DispatchSemaphore(value: 0)
        check("a blocking command is admitted",
              scheduler.enqueue(channel: "blocker") {
                  blockerEntered.signal(); _ = blockerRelease.wait(timeout: .now() + 3)
              })
        check("the blocker entered", blockerEntered.wait(timeout: .now() + 2) == .success)
        let drained = DispatchSemaphore(value: 0)
        for index in 0..<7 {
            check("command \(index) is admitted behind the blocker, under the total depth",
                  scheduler.enqueue(channel: "chan-\(index)") { if index == 6 { drained.signal() } })
        }
        let outstandingAtDepth = scheduler.outstandingForTesting()
        expect("outstanding total reads eight while the lane is full", outstandingAtDepth.total, 8)
        check("a ninth distinct-channel command is refused at the total depth",
              !scheduler.enqueue(channel: "over-depth") {})
        blockerRelease.signal()
        check("the seven queued behind the blocker drain in turn",
              drained.wait(timeout: .now() + 2) == .success)
        check("the lane drains back to zero once every worker finishes",
              eventually { scheduler.outstandingForTesting().total == 0 })

        // Per-channel depth: same shape — a blocker on one channel, a second admission behind it
        // on the same channel, a third refused, and a different channel unaffected.
        let sameEntered = DispatchSemaphore(value: 0)
        let sameRelease = DispatchSemaphore(value: 0)
        check("first same-channel command admitted",
              scheduler.enqueue(channel: "same") {
                  sameEntered.signal(); _ = sameRelease.wait(timeout: .now() + 3)
              })
        check("the first same-channel command entered", sameEntered.wait(timeout: .now() + 2) == .success)
        check("second same-channel command admitted behind the first",
              scheduler.enqueue(channel: "same") {})
        check("a third command on the same channel is refused at the per-channel depth",
              !scheduler.enqueue(channel: "same") {})
        check("a different channel is unaffected by another channel's depth",
              scheduler.enqueue(channel: "other") {})
        sameRelease.signal()
        check("the lane drains back to zero", eventually { scheduler.outstandingForTesting().total == 0 })

        // Nested inline reservation: work already on the worker queue that re-enters the same
        // channel does not double-count it — an unbounded number of re-entries into a channel the
        // outer call already reserved must all be admitted, which would refuse at depth two if
        // each counted separately — while a genuinely new channel discovered along the way is
        // still reserved (and freed) as its own admission.
        let cascadeDone = DispatchSemaphore(value: 0)
        check("outer cascade command admitted",
              scheduler.enqueue(channel: "cascade") {
                  check("re-entering the same channel inline a first time does not double-count it",
                        scheduler.enqueue(channel: "cascade") { })
                  check("re-entering the same channel inline a second time still does not "
                          + "double-count it — a real reservation would refuse at depth two here",
                        scheduler.enqueue(channel: "cascade") { })
                  check("a nested new channel discovered along the way is reserved as its own "
                          + "admission and then released",
                        scheduler.enqueue(channel: "cascade-child") {})
                  cascadeDone.signal()
              })
        _ = cascadeDone.wait(timeout: .now() + 2)
        check("the cascade drains back to zero", eventually { scheduler.outstandingForTesting().total == 0 })

        // Channels are de-duplicated and sorted before admission, so a caller handing the same
        // channel twice reserves it once.
        let dupeDone = DispatchSemaphore(value: 0)
        check("duplicate channels in one call are de-duplicated before admission",
              scheduler.enqueue(channels: ["dupe", "dupe", "dupe"]) { dupeDone.signal() })
        _ = dupeDone.wait(timeout: .now() + 2)
        check("the de-duplicated reservation drains to zero",
              eventually { scheduler.outstandingForTesting().total == 0 })

        // Restart-maintenance rejection/drain: closing admission refuses a newcomer without
        // disturbing already-admitted work; reopening with the wrong id does not reopen it.
        let drainRelease = DispatchSemaphore(value: 0)
        let drainEntered = DispatchSemaphore(value: 0)
        check("work admitted before maintenance closes",
              scheduler.enqueue(channel: "drain-me") {
                  drainEntered.signal(); _ = drainRelease.wait(timeout: .now() + 3)
              })
        _ = drainEntered.wait(timeout: .now() + 2)
        let requestID = UUID().uuidString.lowercased()
        scheduler.setRestartMaintenance(active: true, requestID: requestID)
        check("maintenance refusal names the closing request",
              scheduler.maintenanceRefusal()?.requestID == requestID)
        check("a newcomer is refused while maintenance is active",
              !scheduler.enqueue(channel: "newcomer") {})
        let otherRequestID = UUID().uuidString.lowercased()
        scheduler.setRestartMaintenance(active: false, requestID: otherRequestID)
        check("reopening with a different request id does not reopen admission",
              scheduler.maintenanceRefusal() != nil)
        let drainBeforeRelease = scheduler.drainSnapshot()
        expect("the drain snapshot still reports the admitted work",
               drainBeforeRelease.outstanding, 1)
        drainRelease.signal()
        check("the admitted work still drains to zero while admission stays closed",
              eventually { scheduler.drainSnapshot().outstanding == 0 })
        scheduler.setRestartMaintenance(active: false, requestID: requestID)
        check("reopening with the closing request id reopens admission",
              scheduler.maintenanceRefusal() == nil)
        check("a fresh command is admitted once maintenance clears",
              scheduler.enqueue(channel: "after-maintenance") {})
    }

    group("Orchestrator and RemoteServer keep every W2-1 legacy facade name, delegating to its new owner") {
        Orchestrator.forget()
        let scheduleID = "w2-facade-\(UUID().uuidString)"
        check("Orchestrator.handledScheduleFireForTesting starts nil",
              Orchestrator.handledScheduleFireForTesting(scheduleID) == nil)
        let fire = Date(timeIntervalSince1970: 1_800_020_000)
        ScheduleService.settleDispatch(scheduleID: scheduleID, fire: fire, overCapacity: false)
        check("the legacy Orchestrator facade reads what ScheduleService just wrote",
              Orchestrator.handledScheduleFireForTesting(scheduleID) == fire)

        let terminal = "w2-facade-activity-\(UUID().uuidString)"
        _ = OrchestratorRegistry.withTaskRecords { _ in
            OrchestratorEventPublisher.noteActivityLocked(terminalID: terminal, activityClass: "working")
        }
        expect("the legacy Orchestrator.activityGeneration facade reads the new owner's clock",
               Orchestrator.activityGeneration(ofTerminal: terminal),
               OrchestratorEventPublisher.activityGeneration(ofTerminal: terminal))

        let channel = "w2-facade-\(UUID().uuidString)"
        let done = DispatchSemaphore(value: 0)
        check("RemoteServer.shared.enqueueTerminalCommand still enters the bounded lane",
              RemoteServer.shared.enqueueTerminalCommand(channel: channel) { done.signal() })
        _ = done.wait(timeout: .now() + 2)
        check("RemoteServer.shared.terminalOutstandingForTesting still drains to zero",
              eventually { RemoteServer.shared.terminalOutstandingForTesting().total == 0 })
        RemoteServer.shared.setRestartMaintenance(active: false, requestID: nil)
        check("RemoteServer.shared.terminalMaintenanceRefusal still answers nil once cleared",
              RemoteServer.shared.terminalMaintenanceRefusal() == nil)
        expect("RemoteServer.terminalDepth still names the scheduler's documented ceiling",
               RemoteServer.terminalDepth, TerminalCommandScheduler.depth)
        expect("RemoteServer.terminalChannelDepth still names the scheduler's documented ceiling",
               RemoteServer.terminalChannelDepth, TerminalCommandScheduler.channelDepth)
    }

    group("Orchestrator.swift no longer declares the W2-1 state it handed to its new owners") {
        // Owner uniqueness and the ratchet, read from the source itself rather than trusted from
        // the diff: none of the eleven identifiers this slice moved is declared as a stored
        // property of `Orchestrator` any more, only referenced through the new owner types.
        let source = (try? String(contentsOfFile: "Sources/Orchestrator.swift", encoding: .utf8)) ?? ""
        check("Orchestrator.swift is readable from the suite's working directory", !source.isEmpty)
        let movedDeclarations = [
            "var handledScheduleFires", "var pendingScheduleFires", "var lastMissedScheduleFires",
            "var dispatchingSchedules", "var invalidScheduleFingerprints", "var beatsInFlight",
            "var completionPumpScheduled", "var completionPumpGeneration",
            "var sessionActivityGenerations", "var sessionActivityClasses",
        ]
        for declaration in movedDeclarations {
            check("Orchestrator.swift no longer declares `\(declaration)`",
                  !source.contains("static \(declaration)"))
        }
        let scheduleServiceSource =
            (try? String(contentsOfFile: "Sources/ScheduleService.swift", encoding: .utf8)) ?? ""
        let publisherSource =
            (try? String(contentsOfFile: "Sources/OrchestratorEventPublisher.swift", encoding: .utf8))
            ?? ""
        for declaration in movedDeclarations.prefix(6) {
            check("ScheduleService.swift declares `\(declaration)`",
                  scheduleServiceSource.contains("static \(declaration)"))
        }
        for declaration in movedDeclarations.suffix(4) {
            check("OrchestratorEventPublisher.swift declares `\(declaration)`",
                  publisherSource.contains("static \(declaration)"))
        }
    }
}
