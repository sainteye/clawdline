import Foundation

enum ScheduleResumeTestFailure: Error, CustomStringConvertible {
    case failed(String)

    var description: String {
        switch self {
        case .failed(let message): return message
        }
    }
}

/// Focused regression checks for the schedule-detail exception to the ordinary conversation
/// picker. Kept in its own file because these checks need temporary transcript bytes and several
/// task-registry variants, while the schedule arithmetic table in `main.swift` does not.
///
/// It also holds, at the end, the two claims about the machine-token door — see
/// `checkMachineScheduleDoorClaims`. They belong beside the schedule groups in
/// `Tests/ScheduledDispatchTests.swift`; they are here because that file is a handful of lines
/// under the 2,000-line stop-growth limit, which is that limit doing exactly what it is for.
func runScheduleResumeTests() throws -> Int {
    var checks = 0
    func expect(_ name: String, _ condition: @autoclosure () -> Bool) throws {
        checks += 1
        if !condition() { throw ScheduleResumeTestFailure.failed(name) }
    }

    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-schedule-resume-\(UUID().uuidString)",
                                isDirectory: true)
    let schedules = root.appendingPathComponent("schedules", isDirectory: true)
    let project = root.appendingPathComponent("project", isDirectory: true)
    try FileManager.default.createDirectory(at: schedules, withIntermediateDirectories: true)
    try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
    defer {
        Orchestrator.scheduleDirectoryOverrideForTesting = nil
        Orchestrator.forget()
        try? FileManager.default.removeItem(at: root)
    }

    let scheduleID = "11111111-2222-4333-8444-555555555555"
    let schedule: [String: Any] = [
        "clawdline_schedule": 1,
        "schedule_id": scheduleID,
        "title": "publish",
        "when": ["at": "09:00", "days": "daily"],
        "task": ["assistant": "codex", "project_dir": project.path,
                 "instructions": "publish today's post"],
        "enabled": true,
    ]
    try JSONSerialization.data(withJSONObject: schedule)
        .write(to: schedules.appendingPathComponent("\(scheduleID).json"))
    Orchestrator.scheduleDirectoryOverrideForTesting = schedules
    Orchestrator.forget()

    let sessionID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let rollout = root.appendingPathComponent("rollout-\(sessionID).jsonl")
    try Data("proved earlier".utf8).write(to: rollout)

    var finished = Orchestrator.Task(
        id: "22222222-3333-4444-8555-666666666666", state: .success,
        kind: "custom", title: "publish", assistant: .codex,
        projectDir: project.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 200),
        secretHash: Orchestrator.hash(ofSecret: "finished"))
    finished.scheduleID = scheduleID
    finished.finishedAt = Date(timeIntervalSince1970: 230)
    finished.childTerminalId = "TERMINAL-FINISHED"
    finished.childSessionId = sessionID
    finished.transcriptPath = rollout.path
    finished.transcriptProven = true
    finished.summary = "published post 42"
    Orchestrator.holdScheduleTaskForTesting(finished)

    var active = Orchestrator.Task(
        id: "33333333-4444-4555-8666-777777777777", state: .briefed,
        kind: "custom", title: "publish", assistant: .codex,
        projectDir: project.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 300),
        secretHash: Orchestrator.hash(ofSecret: "active"))
    active.scheduleID = scheduleID
    active.childTerminalId = "TERMINAL-ACTIVE"
    active.childSessionId = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
    active.transcriptPath = rollout.path
    active.transcriptProven = true
    Orchestrator.holdScheduleTaskForTesting(active)

    guard let detail = Orchestrator.scheduleRecord(
        id: scheduleID, now: Date(timeIntervalSince1970: 100)) else {
        throw ScheduleResumeTestFailure.failed("schedule detail exists")
    }
    let runs = detail["runs"] as? [[String: Any]] ?? []
    try expect("every retained run is present", runs.count == 2)
    try expect("runs are newest first", runs.first?["task_id"] as? String == active.id)
    let finishedRun = runs.first { $0["task_id"] as? String == finished.id }
    let activeRun = runs.first { $0["task_id"] as? String == active.id }
    try expect("terminal history exposes a proved conversation",
               finishedRun?["session_id"] as? String == sessionID)
    try expect("an active task is open-only, never resumable",
               activeRun?["session_id"] == nil)
    try expect("the run summary survives into schedule detail",
               finishedRun?["summary"] as? String == "published post 42")

    try expect("the exact scheduled conversation may resume",
               Orchestrator.scheduledResumeTitle(
                sessionID: sessionID, assistant: .codex, projectDir: project.path) != nil)
    try expect("that resume carries the retained task title into its new terminal",
               Orchestrator.scheduledResumeTitle(
                sessionID: sessionID, assistant: .codex, projectDir: project.path) == "publish")
    try expect("another assistant cannot borrow the conversation",
               Orchestrator.scheduledResumeTitle(
                sessionID: sessionID, assistant: .claude, projectDir: project.path) == nil)
    try expect("another project cannot borrow the conversation",
               Orchestrator.scheduledResumeTitle(
                sessionID: sessionID, assistant: .codex, projectDir: root.path) == nil)

    finished.childSessionId = "not-a-session-id"
    Orchestrator.holdScheduleTaskForTesting(finished)
    let malformedDetail = Orchestrator.scheduleRecord(
        id: scheduleID, now: Date(timeIntervalSince1970: 100))
    let malformedRuns = malformedDetail?["runs"] as? [[String: Any]] ?? []
    let malformed = malformedRuns.first { $0["task_id"] as? String == finished.id }
    try expect("a proved but non-resumable identifier is never offered",
               malformed?["session_id"] == nil)
    finished.childSessionId = sessionID
    Orchestrator.holdScheduleTaskForTesting(finished)

    try FileManager.default.removeItem(at: rollout)
    try expect("a removed rollout revokes the resume exception",
               Orchestrator.scheduledResumeTitle(
                sessionID: sessionID, assistant: .codex, projectDir: project.path) == nil)
    let afterRemoval = Orchestrator.scheduleRecord(
        id: scheduleID, now: Date(timeIntervalSince1970: 100))
    let removedRuns = afterRemoval?["runs"] as? [[String: Any]] ?? []
    let removed = removedRuns.first { $0["task_id"] as? String == finished.id }
    try expect("detail stops handing out a missing conversation id",
               removed?["session_id"] == nil)

    try checkMachineScheduleDoorClaims { name, ok in
        checks += 1
        if !ok { throw ScheduleResumeTestFailure.failed(name) }
    }
    return checks
}

/// **What the machine-token door admits, and what the pages may no longer claim about it.**
///
/// The delivery this corrects justified refusing that token a *repeating* schedule as protection
/// against unattended nightly recurrence, and said so in four passages. It is not that. The gate
/// reads one request at a time and counts nothing, so the session a one-shot opens can post the
/// next, and the night after that one can post another — recurrence by composition, with no
/// repeating file anywhere. Nor did the refusal bound it before this door existed: the session
/// that hit it hand-wrote the schedule at mode 0644, and a hand-written file may carry
/// `when.days`. The gate only ever decided which artifact the capability produced.
///
/// So there is nothing here to close, and these checks pin the two things that are true instead:
/// the gate as it really behaves, and the pages as they now read. The second pair is a guard on
/// prose, which is unusual and is the point — prose is what went wrong, and prose is the only
/// part of this delivery nothing could go red about.
private func checkMachineScheduleDoorClaims(_ expect: (String, Bool) throws -> Void) throws {
    try expect("the machine door admits a one-shot and counts nothing that already exists",
               Orchestrator.machineScheduleRefusal(
                method: "POST", id: nil, body: ["on": "2026-09-07", "at": "01:30"]) == nil)
    try expect("while a repeating one is refused whatever else exists",
               Orchestrator.machineScheduleRefusal(
                method: "POST", id: nil, body: ["days": "daily", "at": "01:30"]) != nil)
    let schedulesPage = try String(contentsOfFile: "docs/schedules.md", encoding: .utf8)
    let apiPage = try String(contentsOfFile: "docs/api.md", encoding: .utf8)
    try expect("schedules.md has dropped the sentence saying the token cannot pass those gates",
               !schedulesPage.contains("is not a way past those"))
    try expect("and says instead that the second door passes neither of them",
               schedulesPage.contains("neither the device gate nor the write switch"))
    try expect("and says what the refusal does buy, rather than what it does not",
               schedulesPage.contains("What the refusal does buy"))
    try expect("api.md calls the write switch a device-door answer on the schedule routes",
               apiPage.contains("device-door answer"))
    try expect("and neither page still offers nightly recurrence as the reason for the refusal",
               !schedulesPage.contains("could arrange to be woken every night forever")
                   && !apiPage.contains("woken every night"))
}
