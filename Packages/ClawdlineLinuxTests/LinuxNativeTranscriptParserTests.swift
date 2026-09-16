import Foundation
import XCTest
@testable import ClawdlineApplication
@testable import ClawdlineLinux

// Fixed synthetic vectors only. No test here reads a provider home, procfs or a real transcript;
// the bytes below are the plan's comparison subject (`%42`, `/dev/pts/7`, PID 4242, conversation
// `aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee`) written by hand.
private enum NativeWireFixture {
    static let conversationID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    static let incarnation = LinuxSHA256.hex(Data([
        "11111111-2222-4333-8444-555555555555", "%42", "/dev/pts/7", "4242", "987654321", "4242"
    ].joined(separator: "\u{0}").utf8))
    static let limit = 200

    static func evidence(_ assistant: Assistant, bytes: Data, offset: UInt64 = 0,
                         mode: LinuxNativeTranscriptConversationMode = .root,
                         conversation: String = conversationID,
                         incarnation: String = incarnation, device: UInt64 = 64_769,
                         inode: UInt64 = 1_048_577,
                         modified: Int64 = 1_789_520_409_123_456_789) -> LinuxNativeTranscriptEvidence {
        LinuxNativeTranscriptEvidence(
            assistant: assistant, conversationMode: mode, providerConversationID: conversation,
            terminalIncarnation: incarnation, fileDevice: device, fileInode: inode,
            fileSize: offset + UInt64(bytes.count), fileModifiedNanoseconds: modified,
            suffixOffset: offset)
    }

    static func canonical(_ object: Any) -> String {
        let data = try! JSONSerialization.data(withJSONObject: object,
                                               options: [.sortedKeys, .withoutEscapingSlashes])
        return String(decoding: data, as: UTF8.self)
    }

    static func canonical(json: String) -> String {
        canonical(try! JSONSerialization.jsonObject(with: Data(json.utf8)))
    }

    /// Read the Mac serializer that owns the public row shape. This prevents the Linux test from
    /// merely comparing a Linux constant with itself while the current Mac owner drifts.
    static func currentMacTranscriptRowKeys() throws -> Set<String> {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let source = try String(contentsOf: root.appendingPathComponent("Sources/RemoteServer.swift"),
                                encoding: .utf8)
        guard let start = source.range(of: "static func transcriptRows("),
              let end = source.range(of: "private static func name(",
                                     range: start.upperBound..<source.endIndex) else {
            throw NSError(domain: "NativeWireFixture", code: 1)
        }
        let body = String(source[start.lowerBound..<end.lowerBound])
        let regex = try NSRegularExpression(pattern: #"row\[\"([^\"]+)\"\]"#)
        var keys: Set<String> = ["role", "text"]
        for match in regex.matches(in: body, range: NSRange(body.startIndex..., in: body)) {
            guard let range = Range(match.range(at: 1), in: body) else { continue }
            keys.insert(String(body[range]))
        }
        return keys
    }

    static let claudeLines: [String] = [
        ##"{"type":"summary","summary":"SUMMARY BOOKKEEPING"}"##,
        ##"{"type":"user","timestamp":"2026-09-16T01:00:00.000Z","sessionId":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","message":{"role":"user","content":"<system-reminder>internal</system-reminder>Please **list** the files.\n\n- one\n- two"}}"##,
        ##"{"type":"assistant","isSidechain":true,"timestamp":"2026-09-16T01:00:00.500Z","message":{"content":[{"type":"text","text":"SIDECHAIN PROSE"}]}}"##,
        ##"{"type":"user","isMeta":true,"timestamp":"2026-09-16T01:00:00.600Z","message":{"content":"META PROSE"}}"##,
        ##"{"type":"assistant","timestamp":"2026-09-16T01:00:00.700Z","message":{"content":[{"type":"thinking","thinking":"THINKING PROSE","signature":"EgpCCHRoaW5raW5n"},{"type":"thinking","thinking":"   "}]}}"##,
        ##"{"type":"assistant","timestamp":"2026-09-16T01:00:01.500Z","message":{"content":[{"type":"text","text":"Here is the plan:\n\n```sh\nls -la\n```"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la\npwd","description":"List"}}]}}"##,
        ##"{"type":"user","timestamp":"2026-09-16T01:00:02.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"\u001b[32mREADME.md\u001b[0m\nPackage.swift"}]}]}}"##,
        ##"{"type":"assistant","timestamp":"2026-09-16T01:00:03.000Z","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Write","input":{"file_path":"/var/lib/clawdline-projects/reaver/notes.md","content":"# Notes\n"}}]}}"##,
        ##"{"type":"user","timestamp":"2026-09-16T01:00:03.100Z","toolUseResult":{"type":"create","filePath":"/var/lib/clawdline-projects/reaver/notes.md","content":"# Notes\n"},"message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"File created successfully"}]}}"##,
        ##"{"type":"assistant","timestamp":"2026-09-16T01:00:04.000Z","message":{"content":[{"type":"tool_use","id":"toolu_3","name":"AskUserQuestion","input":{"questions":[{"question":"Ship it?","header":"Release","options":[{"label":"Yes","description":"Land now"}]}]}}]}}"##,
        ##"{"type":"queue-operation","operation":"enqueue","timestamp":"2026-09-16T01:00:05.000Z","content":"Also check [Image #1] the logs"}"##,
        ##"{"type":"user","isMeta":true,"timestamp":"2026-09-16T01:00:06.000Z","origin":{"kind":"peer","body":"Review done.","name":"Reviewer","fromMode":"plan","msg_id":"m1"}}"##,
        ##"{"type":"user","timestamp":"2026-09-16T01:00:06.500Z","message":{"content":"<clawdline-notice>{\"v\":1}</clawdline-notice>"}}"##,
        ##"{"type":"user","message":"##,
        ##"not json at all"##,
        ##"{"type":"system","timestamp":"2026-09-16T01:00:07.000Z","content":"<command-name>/model</command-name><command-args>fable</command-args>"}"##,
        ##"{"type":"assistant","timestamp":"2026-09-16T01:00:08.000Z","message":{"content":[{"type":"thinking","thinking":"Checking the tests now.","signature":"EgpCCW5hcnJhdGlvbg=="}]}}"##
    ]
    static let claudeTorn = ##"{"type":"assistant","timestamp":"2026-09-16T01:00:09.000Z","message":{"content":[{"type":"text","text":"TORN"##
    static var claudeBytes: Data { Data((claudeLines.joined(separator: "\n") + "\n" + claudeTorn).utf8) }

    static let claudeExpected = #"""
    [
     {"role":"user","text":"Please **list** the files.\n\n- one\n- two","at":1789520400,"imageCount":0},
     {"role":"assistant","text":"Here is the plan:\n\n```sh\nls -la\n```","at":1789520401},
     {"role":"tool","text":"ls -la","tool":"Bash","at":1789520401},
     {"role":"tool","text":"README.md","at":1789520402},
     {"role":"tool","text":"/var/lib/clawdline-projects/reaver/notes.md","tool":"Write","at":1789520403,
      "fileChanges":[{"path":"/var/lib/clawdline-projects/reaver/notes.md","kind":"write","content":"# Notes\n"}]},
     {"role":"tool","text":"\u0001ask\u0001[{\"h\":\"Release\",\"o\":[{\"d\":\"Land now\",\"l\":\"Yes\"}],\"q\":\"Ship it?\"}]","tool":"AskUserQuestion","at":1789520404},
     {"role":"user","text":"Also check the logs","at":1789520405,"imageCount":1},
     {"role":"peer","text":"Review done.","at":1789520406,"source":"Reviewer","sourceMode":"plan"},
     {"role":"user","text":"<clawdline-notice>{\"v\":1}</clawdline-notice>","at":1789520406,"imageCount":0},
     {"role":"user","text":"/model fable","at":1789520407,"imageCount":0},
     {"role":"assistant","text":"Checking the tests now.","at":1789520408}
    ]
    """#

    static let codexLines: [String] = [
        ##"{"timestamp":"2026-09-16T02:00:00.000Z","type":"session_meta","payload":{"session_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","cwd":"/var/lib/clawdline-projects/reaver","originator":"codex-tui","source":"cli","thread_source":"user"}}"##,
        ##"{"timestamp":"2026-09-16T02:00:01.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"Add a **plan** for the fix"}]}}}"##,
        ##"{"timestamp":"2026-09-16T02:00:01.200Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"Reasoning","summary":[{"type":"summary_text","text":"REASONING PROSE"}]}}}"##,
        ##"{"timestamp":"2026-09-16T02:00:01.300Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"RESPONSE ITEM PROSE"}]}}"##,
        ##"{"timestamp":"2026-09-16T02:00:02.000Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"const p=[{step:\"Read code\",status:\"completed\"},{step:\"Write fix\",status:\"in_progress\"}]; tools.update_plan({plan:p})"}}"##,
        ##"{"timestamp":"2026-09-16T02:00:03.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["/bin/bash","-lc","swift test --filter X"],"aggregated_output":"Build complete!\nTest passed","exit_code":0,"status":"completed"}}}"##,
        ##"{"timestamp":"2026-09-16T02:00:04.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["/bin/bash","-lc","sed -n 1,20p README.md"],"parsed_cmd":[{"type":"read","cmd":"sed -n 1,20p README.md","name":"README.md","path":"README.md"}],"status":"completed","duration":{"secs":1,"nanos":250000000}}}}"##,
        ##"{"timestamp":"2026-09-16T02:00:05.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","server":"docs","tool":"search","arguments":{"title":"Search docs"},"result":{"content":[{"type":"text","text":"Found 2"}]},"status":"completed"}}}"##,
        ##"{"timestamp":"2026-09-16T02:00:06.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","changes":{"/var/lib/clawdline-projects/reaver/docs/NEW.md":{"type":"add","content":"# New\n"},"/var/lib/clawdline-projects/reaver/Sources/App.swift":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n"}}}}}"##,
        ##"{"timestamp":"2026-09-16T02:00:07.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"output_text","text":"Done.\n\n| a | b |\n|---|---|"}]}}}"##
    ]
    static let codexTorn = ##"{"timestamp":"2026-09-16T02:00:08.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"output_text","text":"TORN"##
    static var codexBytes: Data { Data((codexLines.joined(separator: "\n") + "\n" + codexTorn).utf8) }

    static let codexExpected = #"""
    [
     {"role":"user","text":"Add a **plan** for the fix","at":1789524001,"imageCount":0},
     {"role":"tool","text":"Updated Plan","tool":"plan","at":1789524002,
      "plan":[{"step":"Read code","status":"completed"},{"step":"Write fix","status":"inProgress"}]},
     {"role":"tool","text":"swift test --filter X","tool":"shell","at":1789524003},
     {"role":"tool","text":"Build complete!","at":1789524003},
     {"role":"tool","text":"Read README.md","tool":"shell","at":1789524004,
      "activity":{"kind":"explored","status":"completed","durationMs":1250,
       "actions":[{"kind":"read","command":"sed -n 1,20p README.md","name":"README.md","path":"README.md"}]}},
     {"role":"tool","text":"Search docs","tool":"docs.search","at":1789524005,
      "activity":{"kind":"called","title":"Search docs","status":"completed","result":"Found 2","actions":[]}},
     {"role":"tool","text":"App.swift, NEW.md","tool":"edit","at":1789524006,
      "fileChanges":[
       {"path":"/var/lib/clawdline-projects/reaver/Sources/App.swift","kind":"update","unifiedDiff":"@@ -1 +1 @@\n-old\n+new\n"},
       {"path":"/var/lib/clawdline-projects/reaver/docs/NEW.md","kind":"add","content":"# New\n"}]},
     {"role":"assistant","text":"Done.\n\n| a | b |\n|---|---|","at":1789524007}
    ]
    """#
}

extension LinuxRuntimeContractTests {
    func testNativeWireClaudeVectorProducesExactWebRows() throws {
        let bytes = NativeWireFixture.claudeBytes
        let parse = try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: NativeWireFixture.evidence(.claude, bytes: bytes),
            limit: NativeWireFixture.limit)
        let rows = LinuxNativeTranscriptWire.webRows(parse.entries)
        XCTAssertEqual(NativeWireFixture.canonical(rows),
                       NativeWireFixture.canonical(json: NativeWireFixture.claudeExpected))
        XCTAssertEqual(parse.providerConversationID, NativeWireFixture.conversationID)
        let texts = rows.compactMap { $0["text"] as? String }.joined(separator: "\n")
        for invented in ["SIDECHAIN PROSE", "META PROSE", "THINKING PROSE", "SUMMARY BOOKKEEPING",
                         "TORN", "system-reminder", "Package.swift"] {
            XCTAssertFalse(texts.contains(invented), invented)
        }
        // A tool result is its own `tool` row, never folded into the person's turn.
        guard rows.count == 11 else { return XCTFail("expected 11 Claude rows, got \(rows.count)") }
        XCTAssertEqual(rows[3]["role"] as? String, "tool")
        XCTAssertNil(rows[3]["tool"])
        XCTAssertNil(rows[3]["imageCount"])
        for row in rows { XCTAssertTrue(Set(row.keys).isSubset(of: LinuxNativeTranscriptWire.webRowKeys)) }
        let body = LinuxNativeTranscriptWire.webBody(parse)
        XCTAssertEqual(Set(body.keys), ["entries", "signature"])
        XCTAssertEqual(body["signature"] as? String, parse.signature)
    }

    func testNativeWireCodexVectorProducesExactWebRows() throws {
        let bytes = NativeWireFixture.codexBytes
        let parse = try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: NativeWireFixture.evidence(.codex, bytes: bytes),
            limit: NativeWireFixture.limit)
        let rows = LinuxNativeTranscriptWire.webRows(parse.entries)
        XCTAssertEqual(NativeWireFixture.canonical(rows),
                       NativeWireFixture.canonical(json: NativeWireFixture.codexExpected))
        let texts = rows.compactMap { $0["text"] as? String }.joined(separator: "\n")
        for invented in ["REASONING PROSE", "RESPONSE ITEM PROSE", "TORN", "Test passed"] {
            XCTAssertFalse(texts.contains(invented), invented)
        }
        for row in rows { XCTAssertTrue(Set(row.keys).isSubset(of: LinuxNativeTranscriptWire.webRowKeys)) }
    }

    func testNativeWireSerializerOwnsEveryMacRowKey() throws {
        let entry = LinuxTranscriptEntry(
            kind: .message, text: "Body", tool: "t", at: 7, imageCount: 3, source: "Root",
            sourceMode: "clawdline", sourceAssistant: .codex,
            artifacts: [["id": .string("img-1"), "width": .integer(4)]],
            notice: ["kind": .string("done"), "ok": .bool(true)],
            fileChanges: [LinuxTranscriptFileChange(path: "/p", kind: "update", unifiedDiff: "d",
                                                    content: "c", movePath: "/q")],
            plan: [LinuxTranscriptPlanStep(step: "s", status: "pending")],
            activity: LinuxTranscriptActivity(kind: "called", title: "T", status: "failed",
                                              durationMilliseconds: 9, result: "r",
                                              actions: [LinuxTranscriptToolAction(kind: "search", query: "q")]))
        let row = LinuxNativeTranscriptWire.webRows([entry])[0]
        let macKeys = try NativeWireFixture.currentMacTranscriptRowKeys()
        XCTAssertEqual(LinuxNativeTranscriptWire.webRowKeys, macKeys)
        // Only a user row carries imageCount, exactly as `RemoteServer.transcriptRows`.
        XCTAssertEqual(Set(row.keys), macKeys.subtracting(["imageCount"]))
        XCTAssertEqual(NativeWireFixture.canonical(row), NativeWireFixture.canonical(json: #"""
        {"role":"message","text":"Body","tool":"t","at":7,"source":"Root","sourceMode":"clawdline",
         "sourceAssistant":"codex","artifacts":[{"id":"img-1","width":4}],"notice":{"kind":"done","ok":true},
         "fileChanges":[{"path":"/p","kind":"update","unifiedDiff":"d","content":"c","movePath":"/q"}],
         "plan":[{"step":"s","status":"pending"}],
         "activity":{"kind":"called","title":"T","status":"failed","durationMs":9,"result":"r",
                     "actions":[{"kind":"search","query":"q"}]}}
        """#))
        var user = entry
        user.kind = .user
        XCTAssertEqual(LinuxNativeTranscriptWire.webRows([user])[0]["imageCount"] as? Int, 3)
    }

    func testNativeWireSignatureBindsEveryEvidenceField() throws {
        let bytes = NativeWireFixture.claudeBytes
        let base = NativeWireFixture.evidence(.claude, bytes: bytes)
        let signature = try LinuxNativeTranscriptWire.parse(bytes: bytes, evidence: base,
                                                            limit: 200).signature
        XCTAssertTrue(LinuxNativeTranscriptWire.isSHA256Hex(signature))
        XCTAssertEqual(signature, try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: base, limit: 1).signature)
        let otherConversation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeef"
        let otherIncarnation = LinuxSHA256.hex(Data("another incarnation".utf8))
        var changed = Data(bytes)
        changed[changed.startIndex + 30] ^= 0x01
        let variants: [(String, Data, LinuxNativeTranscriptEvidence)] = [
            ("assistant", bytes, NativeWireFixture.evidence(.codex, bytes: bytes)),
            ("conversation mode", bytes, NativeWireFixture.evidence(
                .claude, bytes: bytes, mode: .claudeAgent)),
            ("conversation", bytes, NativeWireFixture.evidence(.claude, bytes: bytes,
                                                               conversation: otherConversation)),
            ("incarnation", bytes, NativeWireFixture.evidence(.claude, bytes: bytes,
                                                              incarnation: otherIncarnation)),
            ("device", bytes, NativeWireFixture.evidence(.claude, bytes: bytes, device: 64_770)),
            ("inode", bytes, NativeWireFixture.evidence(.claude, bytes: bytes, inode: 1_048_578)),
            ("mtime", bytes, NativeWireFixture.evidence(.claude, bytes: bytes,
                                                        modified: 1_789_520_409_123_456_790)),
            ("offset and size", bytes, NativeWireFixture.evidence(.claude, bytes: bytes, offset: 0)),
            ("bytes", changed, NativeWireFixture.evidence(.claude, bytes: changed))
        ]
        for (name, variantBytes, evidence) in variants {
            let other = LinuxNativeTranscriptWire.signature(bytes: variantBytes, evidence: evidence)
            if name == "offset and size" { XCTAssertEqual(other, signature, name); continue }
            XCTAssertNotEqual(other, signature, name)
        }
        let shifted = LinuxNativeTranscriptEvidence(
            assistant: .claude, conversationMode: .root,
            providerConversationID: base.providerConversationID,
            terminalIncarnation: base.terminalIncarnation, fileDevice: base.fileDevice,
            fileInode: base.fileInode, fileSize: base.fileSize + 1,
            fileModifiedNanoseconds: base.fileModifiedNanoseconds, suffixOffset: 1)
        XCTAssertNotEqual(LinuxNativeTranscriptWire.signature(bytes: bytes, evidence: shifted),
                          signature, "size and offset")
    }

    func testNativeWireFailsClosedOnEvidenceAndHeadMismatch() throws {
        let bytes = NativeWireFixture.claudeBytes
        func failure(_ body: () throws -> Void) -> LinuxNativeTranscriptWireError? {
            do { try body(); return nil } catch { return error as? LinuxNativeTranscriptWireError }
        }
        for bad in ["", "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE", "../aaaaaaaa-bbbb-4ccc-8ddd-eeeee",
                    "aaaaaaaa/bbbb-4ccc-8ddd-eeeeeeeeeeee", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee.jsonl"] {
            XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
                bytes: bytes, evidence: NativeWireFixture.evidence(.claude, bytes: bytes,
                                                                   conversation: bad),
                limit: 200) }, .invalidConversationID, bad)
        }
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: NativeWireFixture.evidence(.claude, bytes: bytes,
                                                               incarnation: "not-a-digest"),
            limit: 200) }, .invalidTerminalIncarnation)
        let base = NativeWireFixture.evidence(.claude, bytes: bytes)
        let inconsistent = LinuxNativeTranscriptEvidence(
            assistant: .claude, conversationMode: .root,
            providerConversationID: base.providerConversationID,
            terminalIncarnation: base.terminalIncarnation, fileDevice: 1, fileInode: 2,
            fileSize: base.fileSize + 5, fileModifiedNanoseconds: 3, suffixOffset: 0)
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: inconsistent, limit: 200) }, .evidenceInconsistent)
        let oversized = Data(repeating: 0x0A, count: LinuxNativeTranscriptWire.maximumSuffixBytes + 1)
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: oversized, evidence: NativeWireFixture.evidence(.claude, bytes: oversized),
            limit: 200) }, .suffixLimitExceeded)
        // The byte ceiling and the row-work ceiling are independent. One committed empty row
        // followed by one torn 8 MiB fragment reaches the byte boundary without requiring the
        // parser to walk millions of physical rows.
        var boundary = Data([0x0A])
        boundary.append(Data(repeating: 0x78,
                             count: LinuxNativeTranscriptWire.maximumSuffixBytes - 1))
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(
            bytes: boundary, evidence: NativeWireFixture.evidence(.claude, bytes: boundary),
            limit: 200).entries, [])
        let noBoundary = Data("{\"type\":\"user\",\"message\":{\"content\":\"FRAGMENT\"}}".utf8)
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: noBoundary, evidence: NativeWireFixture.evidence(.claude, bytes: noBoundary,
                                                                    offset: 4_096),
            limit: 200) }, .suffixLimitExceeded)
        XCTAssertEqual(LinuxNativeTranscriptWireError.suffixLimitExceeded.externalCode,
                       "transcript_limit_exceeded")
        XCTAssertEqual(LinuxNativeTranscriptWireError.rowWorkLimitExceeded.externalCode,
                       "transcript_limit_exceeded")
        XCTAssertEqual(LinuxNativeTranscriptWireError.parseDeadlineExceeded.externalCode,
                       "transcript_busy")
        XCTAssertEqual(LinuxNativeTranscriptWireError.conversationMismatch.externalCode,
                       "transcript_identity_unknown")

        let emptyCodex = Data()
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: emptyCodex, evidence: NativeWireFixture.evidence(.codex, bytes: emptyCodex),
            limit: 200) }, .conversationMismatch)
        let malformedCodex = Data("not json\n{also not json}\n".utf8)
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: malformedCodex,
            evidence: NativeWireFixture.evidence(.codex, bytes: malformedCodex), limit: 200)
        }, .conversationMismatch)
        XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
            bytes: NativeWireFixture.codexBytes,
            evidence: NativeWireFixture.evidence(.codex, bytes: NativeWireFixture.codexBytes,
                                                 mode: .claudeAgent), limit: 200)
        }, .invalidConversationMode)

        let head = NativeWireFixture.codexLines[0]
        let rest = NativeWireFixture.codexLines.dropFirst().joined(separator: "\n") + "\n"
        let heads = [
            ("other id", head.replacingOccurrences(of: "eeeeeeeeeeee\",\"cwd", with: "eeeeeeeeeeef\",\"cwd")),
            ("payload.id only", head.replacingOccurrences(of: "\"session_id\":", with: "\"id\":")),
            ("subagent", head.replacingOccurrences(of: "\"thread_source\":\"user\"",
                                                   with: "\"thread_source\":\"subagent\"")),
            ("legacy absent thread_source", head.replacingOccurrences(of: ",\"thread_source\":\"user\"", with: "")),
            ("no head", "")
        ]
        for (name, variant) in heads {
            XCTAssertNotEqual(variant, head, name)
            let data = Data((variant.isEmpty ? rest : variant + "\n" + rest).utf8)
            XCTAssertEqual(failure { _ = try LinuxNativeTranscriptWire.parse(
                bytes: data, evidence: NativeWireFixture.evidence(.codex, bytes: data),
                limit: 200) }, .conversationMismatch, name)
        }
        // A suffix that starts after the head is bound by the identity layer, not re-read here.
        let suffix = Data(("partial\n" + rest).utf8)
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(
            bytes: suffix, evidence: NativeWireFixture.evidence(.codex, bytes: suffix, offset: 9_000),
            limit: 200).entries.count, 8)
    }

    func testNativeWireNonsemanticStableBytesAreSuccessfulEmptyAndLimitsAreBounded() throws {
        let bookkeeping = Data(([
            ##"{"type":"summary","summary":"SUMMARY BOOKKEEPING"}"##,
            ##"{"type":"file-history-snapshot","snapshot":{}}"##,
            NativeWireFixture.claudeLines[2], NativeWireFixture.claudeLines[3],
            NativeWireFixture.claudeLines[4], ##"{"type":"user","message":"##
        ].joined(separator: "\n") + "\n" + NativeWireFixture.claudeTorn).utf8)
        let empty = try LinuxNativeTranscriptWire.parse(
            bytes: bookkeeping, evidence: NativeWireFixture.evidence(.claude, bytes: bookkeeping),
            limit: 200)
        XCTAssertEqual(empty.entries, [])
        XCTAssertTrue(LinuxNativeTranscriptWire.isSHA256Hex(empty.signature))
        // An empty semantic suffix still carries the leading partial-row delimiter. With no
        // delimiter at all the identity layer cannot prove where a nonzero offset starts.
        let freshSuffix = Data("\n".utf8)
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(
            bytes: freshSuffix,
            evidence: NativeWireFixture.evidence(.codex, bytes: freshSuffix, offset: 1),
            limit: 200).entries, [])

        // A suffix beginning after byte zero drops its first line even when that line parses.
        let complete = NativeWireFixture.claudeLines[1] + "\n" + NativeWireFixture.claudeLines[15] + "\n"
        let partial = Data(complete.utf8)
        let suffixRows = try LinuxNativeTranscriptWire.parse(
            bytes: partial, evidence: NativeWireFixture.evidence(.claude, bytes: partial, offset: 77),
            limit: 200).entries
        XCTAssertEqual(suffixRows.map(\.text), ["/model fable"])

        let bytes = NativeWireFixture.claudeBytes
        let evidence = NativeWireFixture.evidence(.claude, bytes: bytes)
        let newest = try LinuxNativeTranscriptWire.parse(bytes: bytes, evidence: evidence, limit: 2)
        XCTAssertEqual(newest.entries.map(\.text), ["/model fable", "Checking the tests now."])
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(bytes: bytes, evidence: evidence,
                                                           limit: 0).entries.map(\.text),
                       ["Checking the tests now."])
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(bytes: bytes, evidence: evidence,
                                                           limit: 5_000).entries.count, 11)
        XCTAssertEqual(LinuxNativeTranscriptWire.entryLimitRange, 1...1_000)
    }

    func testNativeWireClaudeModeSeparatesRootAndAgentConversations() throws {
        let sidechain = Data((NativeWireFixture.claudeLines[2] + "\n").utf8)
        let root = try LinuxNativeTranscriptWire.parse(
            bytes: sidechain, evidence: NativeWireFixture.evidence(.claude, bytes: sidechain),
            limit: 20)
        let agent = try LinuxNativeTranscriptWire.parse(
            bytes: sidechain,
            evidence: NativeWireFixture.evidence(.claude, bytes: sidechain, mode: .claudeAgent),
            limit: 20)
        XCTAssertEqual(root.entries, [])
        XCTAssertEqual(agent.entries.map(\.text), ["SIDECHAIN PROSE"])
        XCTAssertNotEqual(root.signature, agent.signature)

        let rootOnly = Data((NativeWireFixture.claudeLines[1] + "\n").utf8)
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(
            bytes: rootOnly,
            evidence: NativeWireFixture.evidence(.claude, bytes: rootOnly, mode: .claudeAgent),
            limit: 20).entries, [])
    }

    func testNativeWirePreservesTagShapedProseAndHidesUnprovenThinking() throws {
        let rows = [
            ##"{"type":"user","message":{"content":"<clawdline-message>{not decoded}</clawdline-message>"}}"##,
            ##"{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"MISSING SIGNATURE"}]}}"##,
            ##"{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"MALFORMED SIGNATURE","signature":"%%%"}]}}"##,
            ##"{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"EXPLICIT THINKING","signature":"EgpCCHRoaW5raW5n"}]}}"##,
            ##"{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"EXPLICIT NARRATION","signature":"EgpCCW5hcnJhdGlvbg=="}]}}"##
        ]
        let bytes = Data((rows.joined(separator: "\n") + "\n").utf8)
        let entries = try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: NativeWireFixture.evidence(.claude, bytes: bytes),
            limit: 20).entries
        XCTAssertEqual(entries.map(\.text), [
            "<clawdline-message>{not decoded}</clawdline-message>", "EXPLICIT NARRATION"
        ])
    }

    func testNativeWireLimitStopsReverseJSONDecodingAndFinalRowNeedsNewline() throws {
        let older = ##"{"type":"assistant","message":{"content":[{"type":"text","text":""##
            + String(repeating: "x", count: LinuxNativeTranscriptWire.maximumSuffixBytes - 512)
            + ##""}]}}"##
        let newest = ##"{"type":"user","message":{"content":"newest"}}"##
        let bytes = Data((older + "\n" + newest + "\n").utf8)
        XCTAssertLessThanOrEqual(bytes.count, LinuxNativeTranscriptWire.maximumSuffixBytes)
        let limited = try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: NativeWireFixture.evidence(.claude, bytes: bytes), limit: 1)
        XCTAssertEqual(limited.entries.map(\.text), ["newest"])
        XCTAssertEqual(limited.decodedRowCount, 1)

        let committed = ##"{"type":"assistant","message":{"content":[{"type":"text","text":"committed"}]}}"##
        let validButUnterminated = ##"{"type":"assistant","message":{"content":[{"type":"text","text":"torn"}]}}"##
        let tornBytes = Data((committed + "\n" + validButUnterminated).utf8)
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(
            bytes: tornBytes, evidence: NativeWireFixture.evidence(.claude, bytes: tornBytes),
            limit: 20).entries.map(\.text), ["committed"])

        let codexTorn = Data((NativeWireFixture.codexLines[0] + "\n"
            + NativeWireFixture.codexLines[1]).utf8)
        XCTAssertEqual(try LinuxNativeTranscriptWire.parse(
            bytes: codexTorn, evidence: NativeWireFixture.evidence(.codex, bytes: codexTorn),
            limit: 20).entries, [])
    }

    func testNativeWireBoundsMillionsOfSmallRowsAndParseDeadline() throws {
        let repeated = String(repeating: "{}\n",
                              count: (LinuxNativeTranscriptWire.maximumSuffixBytes + 2) / 3)
        let bytes = Data(repeated.utf8.prefix(LinuxNativeTranscriptWire.maximumSuffixBytes))
        XCTAssertEqual(bytes.count, LinuxNativeTranscriptWire.maximumSuffixBytes)
        XCTAssertThrowsError(try LinuxNativeTranscriptWire.parse(
            bytes: bytes, evidence: NativeWireFixture.evidence(.claude, bytes: bytes),
            limit: 200)) { error in
            XCTAssertEqual(error as? LinuxNativeTranscriptWireError, .rowWorkLimitExceeded)
        }

        var now: TimeInterval = 0
        XCTAssertThrowsError(try LinuxNativeTranscriptWire.parse(
            bytes: NativeWireFixture.claudeBytes,
            evidence: NativeWireFixture.evidence(.claude, bytes: NativeWireFixture.claudeBytes),
            limit: 200, maximumDecodedRows: .max,
            monotonicNow: { defer { now += 2 }; return now }, deadline: 1)) { error in
            XCTAssertEqual(error as? LinuxNativeTranscriptWireError, .parseDeadlineExceeded)
        }
    }
}
