#!/usr/bin/env node
// Compile the actual coordinator; stubs cover only its HTTP type dependencies.
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), 'board-capacity-'));
try {
    const harness = join(work, 'main.swift'), binary = join(work, 'capacity');
    writeFileSync(harness, String.raw`
import Foundation
enum RemoteAuth { enum Verdict { case denied, allowed } }
enum RemoteServer {
    struct Request {}
    struct Response {
        var status: Int
        var headers: [String: String] = [:]
        var code: String = ""
        static func status(_ value: Int) -> Self { .init(status: value) }
        static func error(_ status: Int, _ code: String, _ message: String,
                          extra: [String: Any] = [:]) -> Self { .init(status: status, code: code) }
    }
}
enum ProjectBoardHTTP {
    struct Read { func execute() -> RemoteServer.Response { .status(200) } }
    struct Command {
        var actor = "actor", requestID = "id", fingerprint = "body"
        func execute() -> RemoteServer.Response { .status(200) }
    }
    enum Admission { case response(RemoteServer.Response), read(Read), command(Command) }
    static func admit(_ request: RemoteServer.Request, machine: Bool,
                      permission: RemoteAuth.Verdict) -> Admission? { nil }
}
var checks = 0
func check(_ value: Bool, _ label: String) {
    guard value else { print("FAIL: " + label); exit(1) }
    checks += 1; print("✓ " + label)
}
let lane = ProjectBoardRequestCoordinator()
var jobs: [() -> Void] = [], replies: [RemoteServer.Response] = []
var executions = 0
func submit(_ id: String = "same", _ fingerprint: String = "body") {
    lane.startCommand(identity: .init(actor: "actor", requestID: id), fingerprint: fingerprint,
        executor: { jobs.append($0) }, work: { executions += 1; return .status(200) },
        completeOnOwner: { $0() }, deliver: { replies.append($0) })
}
for _ in 0..<100 { submit() }
check(jobs.count == 1, "duplicate storm executes one command")
check(!replies.isEmpty && replies.allSatisfy { $0.status == 429 && $0.code == "board_command_busy" },
      "duplicate waiters have typed bounded admission")
check(lane.state.joinedWaiters <= 7, "at most eight total replies retained per command")
check(replies.allSatisfy { $0.headers["Retry-After"] == "1" }, "refused duplicates can retry")
submit("same", "changed")
check(replies.last?.status == 409, "changed intent conflicts even at waiter capacity")
submit("other")
check(jobs.count == 2, "one duplicate storm cannot take another identity's slot")
var readJobs: [() -> Void] = [], readReplies: [Int] = []
for _ in 0..<10 {
    lane.startRead(executor: { readJobs.append($0) }, work: { .status(200) },
        completeOnOwner: { $0() }, deliver: { readReplies.append($0.status) })
}
check(readJobs.count == 4 && readReplies.count == 6 && readReplies.allSatisfy { $0 == 429 },
      "independent read capacity refuses excess work")
while !jobs.isEmpty { jobs.removeFirst()() }
while !readJobs.isEmpty { readJobs.removeFirst()() }
check(executions == 2 && replies.filter { $0.status == 200 }.count == 9,
      "accepted waiters receive exactly one durable result each")
check(lane.state.commands == 0 && lane.state.reads == 0 && lane.state.joinedWaiters == 0,
      "drain releases all retained capacity")
print("BOARD_CAPACITY_CHECKS=\(checks)")
`);
    const compiled = spawnSync('bash', ['tools/with-compile-lock.sh', 'swiftc',
        resolve('Sources/ProjectBoardRequestCoordinator.swift'), harness, '-o', binary],
    { encoding: 'utf8', maxBuffer: 4 * 1024 * 1024 });
    process.stdout.write(compiled.stdout || '');
    process.stderr.write(compiled.stderr || '');
    if (compiled.status !== 0) throw new Error(`capacity compile: ${compiled.status}`);
    const run = spawnSync(binary, [], { encoding: 'utf8' });
    process.stdout.write(run.stdout || '');
    process.stderr.write(run.stderr || '');
    if (run.status !== 0 || !/^BOARD_CAPACITY_CHECKS=9$/m.test(run.stdout))
        throw new Error(`capacity proof failed: ${run.status}`);
} finally {
    rmSync(work, { recursive: true, force: true });
}
