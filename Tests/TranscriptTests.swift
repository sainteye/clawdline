import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

/// A PNG fixture whose dimensions are pixels, never logical points.
func exactPixelPNG(width: Int, height: Int,
                   rgba: (UInt8, UInt8, UInt8, UInt8)) -> Data? {
    guard width > 0, height > 0,
          let rep = NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
          let pixels = rep.bitmapData else { return nil }
    for y in 0..<height {
        let row = pixels.advanced(by: y * rep.bytesPerRow)
        for x in 0..<width {
            let pixel = row.advanced(by: x * 4)
            pixel[0] = rgba.0
            pixel[1] = rgba.1
            pixel[2] = rgba.2
            pixel[3] = rgba.3
        }
    }
    return rep.representation(using: .png, properties: [:])
}

// MARK: - Terminal escapes









// MARK: - Claude Code transcripts

let sampleTranscript = #"""
{"type":"summary","summary":"ignored"}
{"type":"user","timestamp":"2026-08-16T04:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"add a retry"}]}}
{"type":"assistant","timestamp":"2026-08-16T04:00:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Looking at `upload.rb` now."},{"type":"tool_use","name":"Bash","input":{"command":"rg retry upload.rb","description":"search"}}]}}
{"type":"user","timestamp":"2026-08-16T04:00:07.000Z","message":{"role":"user","content":[{"type":"tool_result","content":"upload.rb:42: no retry\nupload.rb:99: none"}]}}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent chatter"}]}}
{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"bookkeeping"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"Done."}]}}
{"type":"user","message":{"role":"user","content":"plain string content"}}
not json at all
"""#


















// The durable completion postman composes both the parent and root forms through one call to
// `ClawdlineMessage.encode(taskFinishedNotice(…))`, so this group is what stands between the
// untouched-claims reminder and a merge that quietly drops it. It has happened once already: the
// encoded notice replaced two hand-built `let line = "…"` statements that each ended in
// `+ untouchedClaimsNotice(for: task)`, and the whole suite stayed green with the reminder gone,
// because everything else here tests `untouchedClaimsNotice` as a pure function. The reminder is
// prose inside `body` by decision, not a typed field, which is exactly why it needs pinning: no
// schema check can miss it on the way out.

func runTranscriptTests() {
group("ansi: plain text is left alone") {
    check("no escapes detected", !Ansi.hasEscapes("just words"))
    check("escapes detected", Ansi.hasEscapes("a \u{1b}[31mb"))

    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let a = Ansi.attributed("hello", font: font, defaultColor: .red)
    expect("text survives", a.string, "hello")
    expect("one run", a.length, 5)
}

group("ansi: colours") {
    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    func colour(_ s: String, at i: Int) -> NSColor? {
        let a = Ansi.attributed(s, font: font, defaultColor: .white)
        guard i < a.length else { return nil }
        return a.attribute(.foregroundColor, at: i, effectiveRange: nil) as? NSColor
    }

    let basic = Ansi.attributed("\u{1b}[31mred\u{1b}[0m plain", font: font, defaultColor: .white)
    expect("escapes are removed from the text", basic.string, "red plain")
    check("the coloured run is not the default", colour("\u{1b}[31mred\u{1b}[0m plain", at: 0) != NSColor.white)
    expect("reset returns to the default", colour("\u{1b}[31mred\u{1b}[0m plain", at: 5), NSColor.white)

    check("bright colours differ from their base",
          colour("\u{1b}[31ma", at: 0) != colour("\u{1b}[91ma", at: 0))
    check("256-colour is parsed", colour("\u{1b}[38;5;196ma", at: 0) != NSColor.white)
    check("truecolour is parsed", colour("\u{1b}[38;2;10;200;30ma", at: 0) != NSColor.white)
    expect("39 goes back to the default", colour("\u{1b}[31ma\u{1b}[39mb", at: 1), NSColor.white)
    expect("bare [m resets", colour("\u{1b}[31ma\u{1b}[mb", at: 1), NSColor.white)
}

group("ansi: everything that is not colour is dropped") {
    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    func text(_ s: String) -> String { Ansi.attributed(s, font: font, defaultColor: .white).string }

    expect("clear screen", text("\u{1b}[2Jhello"), "hello")
    expect("cursor move", text("\u{1b}[10;20Hhello"), "hello")
    expect("erase line", text("a\u{1b}[Kb"), "ab")
    expect("OSC title ending in BEL", text("\u{1b}]0;my title\u{07}hello"), "hello")
    expect("OSC title ending in ST", text("\u{1b}]0;my title\u{1b}\\hello"), "hello")
    expect("a lone escape is not printed", text("a\u{1b}Mb"), "ab")
    expect("newlines survive", text("a\u{1b}[31m\nb"), "a\nb")
}

group("ansi: bold") {
    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let a = Ansi.attributed("\u{1b}[1mbold\u{1b}[22mplain", font: font, defaultColor: .white)
    let boldFont = a.attribute(.font, at: 0, effectiveRange: nil) as? NSFont
    let plainFont = a.attribute(.font, at: 5, effectiveRange: nil) as? NSFont
    check("bold run uses a different face", boldFont != plainFont)
    expect("22 turns it off again", plainFont, font)
}

group("transcript parsing") {
    let entries = Transcript.parse(sampleTranscript)
    expect("blocks become entries", entries.count, 6)

    expect("first is the user", entries[0].kind, Transcript.Entry.Kind.user)
    expect("user text survives", entries[0].text, "add a retry")
    check("timestamps are read", entries[0].time != nil)

    expect("assistant prose", entries[1].kind, Transcript.Entry.Kind.assistant)
    expect("tool call is its own entry", entries[2].kind, Transcript.Entry.Kind.tool)
    expect("tool name is kept", entries[2].tool, "Bash")
    expect("the command is the summary, not the description", entries[2].text, "rg retry upload.rb")

    expect("tool result", entries[3].kind, Transcript.Entry.Kind.toolResult)
    expect("only the first line of a result", entries[3].text, "upload.rb:42: no retry")

    check("sidechains are skipped", !entries.contains { $0.text == "subagent chatter" })
    check("meta records are skipped", !entries.contains { $0.text == "bookkeeping" })
    check("thinking blocks are skipped", !entries.contains { $0.text == "hidden" })
    check("a string content still parses", entries.contains { $0.text == "plain string content" })
    check("unparseable lines are ignored rather than fatal", true)

    let claudeWrite = #"""
{"type":"assistant","timestamp":"2026-08-31T02:18:28.962Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_write","name":"Write","input":{"file_path":"/tmp/task/artifacts/format-sample.md","content":"cwd: /work\nhead: abc123\ntools: Bash, Write\n"}}]}}
{"type":"user","timestamp":"2026-08-31T02:18:28.972Z","toolUseResult":{"type":"create","filePath":"/tmp/task/artifacts/format-sample.md","content":"cwd: /work\nhead: abc123\ntools: Bash, Write\n","structuredPatch":[],"originalFile":null,"userModified":false},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_write","content":"File created successfully at: /tmp/task/artifacts/format-sample.md"}]}}
"""#
    let writeEntries = Transcript.parse(claudeWrite)
    expect("a completed Claude Write is one transcript entry, not call plus receipt",
           writeEntries.count, 1)
    expect("the rich file entry keeps Claude's own tool name", writeEntries.first?.tool, "Write")
    expect("the rich file entry keeps its full path", writeEntries.first?.text,
           "/tmp/task/artifacts/format-sample.md")
    expect("a Claude Write carries one structured file change",
           writeEntries.first?.fileChanges.count, 1)
    expect("the Write content survives losslessly",
           writeEntries.first?.fileChanges.first?.content,
           "cwd: /work\nhead: abc123\ntools: Bash, Write\n")
    expect("the Write change uses a distinct truthful kind",
           writeEntries.first?.fileChanges.first?.kind, "write")
    let writeWire = RemoteServer.transcriptRows(writeEntries).first
    expect("Claude file changes cross the same Web wire as Codex edits",
           (writeWire?["fileChanges"] as? [[String: Any]])?.first?["kind"] as? String,
           "write")
    check("the successful create receipt is not repeated as another tool row",
          !writeEntries.contains { $0.text.contains("File created successfully") })

    expect("the limit keeps the tail", Transcript.parse(sampleTranscript, limit: 2).count, 2)

    // It reads backwards from the newest line and stops as soon as it has enough, because
    // parsing every line of an eight-megabyte tail to keep four hundred entries was a third of a
    // second on the path a session switch runs. What comes back has to be indistinguishable from
    // having read the whole thing: the *newest* entries, still in the order they were written.
    let tail2 = Transcript.parse(sampleTranscript, limit: 2)
    expect("the tail is the newest, not the oldest", tail2.last?.text, "plain string content")
    expect("and it is still in reading order", tail2.first?.text, "Done.")

    // One row can yield several entries, so the limit can be reached in the middle of a row.
    let tail4 = Transcript.parse(sampleTranscript, limit: 4)
    expect("a row that straddles the limit is trimmed to it", tail4.count, 4)
    expect("from the newest end", tail4.map(\.text).suffix(2).joined(separator: "|"),
           "Done.|plain string content")

    // A last line with no newline after it is the normal shape of a file being appended to.
    let unterminated = #"{"type":"user","message":{"role":"user","content":"last word"}}"#
    expect("a line with no newline after it is still read",
           Transcript.parse(sampleTranscript + "\n" + unterminated).last?.text, "last word")
    expect("a single line with no newline at all parses",
           Transcript.parse(unterminated).count, 1)
    expect("blank lines between records are not entries",
           Transcript.parse("\n\n" + unterminated + "\n\n").count, 1)
    expect("nothing at all is nothing", Transcript.parse("").count, 0)

    let imageTurns = [
        #"{"type":"user","timestamp":"2026-08-16T04:00:08.000Z","message":{"role":"user","content":[{"type":"text","text":"describe this[Image #1]"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}}"#,
        #"{"type":"user","timestamp":"2026-08-16T04:00:09.000Z","message":{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}}"#,
    ].joined(separator: "\n")
    let imageEntries = Transcript.parse(imageTurns)
    expect("Claude text and image remain one user turn", imageEntries.count, 2)
    expect("Claude text and image preserve the text", imageEntries.first?.text, "describe this")
    expect("Claude text and image expose one image", imageEntries.first?.imageCount, 1)
    expect("Claude image-only input remains a user turn", imageEntries.last?.kind,
           Transcript.Entry.Kind.user)
    expect("Claude image-only input has a visible attachment marker",
           imageEntries.last?.text, "[Image #1]")
    expect("Claude image-only input exposes one image", imageEntries.last?.imageCount, 1)

    let drop = Drop.directory.appendingPathComponent(
        "clawdline-20260827-120000-000-AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE.png").path
    let pathTurn = #"{"type":"user","timestamp":"2026-08-16T04:00:10.000Z","message":{"role":"user","content":"see docs/notes"}}"#
        .replacingOccurrences(of: "see docs/notes", with: "see docs/notes" + drop)
    let pathEntries = Transcript.parse(pathTurn)
    expect("Claude path fallback remains one user turn", pathEntries.count, 1)
    expect("Claude path fallback preserves authored slashes", pathEntries.first?.text,
           "see docs/notes")
    expect("Claude path fallback exposes one image", pathEntries.first?.imageCount, 1)

    let queued = #"{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-24T10:28:54.573Z","content":"但我按下去瞬間，會出現等待畫面"}"#
    let queuedEntries = Transcript.parse(queued)
    expect("queued input becomes one transcript entry", queuedEntries.count, 1)
    expect("queued input belongs to the user", queuedEntries.first?.kind,
           Transcript.Entry.Kind.user)
    expect("queued input keeps its text", queuedEntries.first?.text,
           "但我按下去瞬間，會出現等待畫面")
    expect("queued input keeps its timestamp",
           queuedEntries.first?.time?.timeIntervalSince1970, 1787567334.573)

    func queuedImageTurn(_ content: String) -> String {
        let object: [String: Any] = [
            "type": "queue-operation", "operation": "enqueue",
            "timestamp": "2026-08-16T04:00:11.000Z", "content": content,
        ]
        let data = try! JSONSerialization.data(withJSONObject: object)
        return String(data: data, encoding: .utf8)!
    }
    let queuedMarker = Transcript.parse(queuedImageTurn("busy [Image #1]"))
    expect("queued Claude marker remains one user turn", queuedMarker.count, 1)
    expect("queued Claude marker is canonical text", queuedMarker.first?.text, "busy")
    expect("queued Claude marker exposes one image", queuedMarker.first?.imageCount, 1)

    let queuedImageOnly = Transcript.parse(queuedImageTurn("[Image #1]"))
    expect("queued Claude image-only input remains a user turn", queuedImageOnly.first?.kind,
           Transcript.Entry.Kind.user)
    expect("queued Claude image-only input keeps a visible marker",
           queuedImageOnly.first?.text, "[Image #1]")
    expect("queued Claude image-only input exposes one image",
           queuedImageOnly.first?.imageCount, 1)

    let queueAuthored = "fix Resources/web/app/js/view/waits.js; see docs/notes; "
        + "check https://example.com/x; keep 'quotes'"
    let queuedPath = Transcript.parse(queuedImageTurn(queueAuthored + drop))
    expect("queued Claude drop path preserves authored slashes, URL, and quotes",
           queuedPath.first?.text, queueAuthored)
    expect("queued Claude drop path exposes one image", queuedPath.first?.imageCount, 1)
    let queuedAdjacent = Transcript.parse(queuedImageTurn("two" + drop + drop))
    expect("queued Claude adjacent paths preserve preceding text",
           queuedAdjacent.first?.text, "two")
    expect("queued Claude adjacent paths expose both images",
           queuedAdjacent.first?.imageCount, 2)

    let literalMarker = Transcript.Entry(kind: .user, text: "literal [Image #1]", tool: nil,
                                         time: Date(timeIntervalSince1970: 100))
    let literalRow = RemoteServer.transcriptRows([literalMarker]).first!
    let literalWireData = try! JSONSerialization.data(withJSONObject: literalRow)
    let literalWire = try! JSONSerialization.jsonObject(with: literalWireData) as! [String: Any]
    expect("a current user wire row explicitly carries zero images",
           literalWire["imageCount"] as? Int, 0)
    expect("a current zero-image wire row preserves an authored marker",
           literalWire["text"] as? String, "literal [Image #1]")

    let malformedQueueRows = [
        #"{"type":"queue-operation","operation":"enqueue"}"#,
        #"{"type":"queue-operation","operation":"enqueue","content":{"text":"not a string"}}"#,
        #"{"type":"queue-operation","operation":"dequeue","content":"not a message"}"#,
        #"{"type":"user","message":{"role":"user","content":"still parses"}}"#,
    ].joined(separator: "\n")
    let afterMalformedQueue = Transcript.parse(malformedQueueRows)
    expect("malformed queue rows do not break later parsing", afterMalformedQueue.count, 1)
    expect("the valid row after malformed queue rows survives", afterMalformedQueue.first?.text,
           "still parses")

    let systemRows = [
        #"{"type":"system","timestamp":"2026-08-24T10:28:54.573Z","content":"<command-name>/rename</command-name>\n<command-message>rename</command-message>\n<command-args>修正瀏覽器問答</command-args>"}"#,
        #"{"type":"system","content":"ordinary system bookkeeping"}"#,
        #"{"type":"system"}"#,
        #"{"type":"system","content":{"text":"not a string"}}"#,
        #"{"type":"system","content":"<command-name>never closes"}"#,
        #"{"type":"system","content":"<command-name>/rename</command-name><command-args>never closes"}"#,
        #"{"type":"system","content":"<local-command-stdout>Session renamed to: 修正瀏覽器問答</local-command-stdout>"}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":"still parses too"}}"#,
    ].joined(separator: "\n")
    let afterSystemRows = Transcript.parse(systemRows)
    expect("a tagged system slash command and the ordinary message survive",
           afterSystemRows.count, 2)
    expect("a system slash command belongs to the user", afterSystemRows.first?.kind,
           Transcript.Entry.Kind.user)
    expect("a system slash command is reconstructed as one line", afterSystemRows.first?.text,
           "/rename 修正瀏覽器問答")
    expect("a system slash command keeps its timestamp",
           afterSystemRows.first?.time?.timeIntervalSince1970, 1787567334.573)
    expect("unrelated and malformed system rows do not break later parsing",
           afterSystemRows.last?.text, "still parses too")

    // The scan walks the UTF-8 view a byte at a time and then slices the String with the indices
    // it stopped on. Those stops are always just after an ASCII newline, so they are always on a
    // character boundary — but if that ever stopped being true the slice would not be wrong, it
    // would trap, and it would trap in the pane rather than in a test. So: lines of characters
    // that are three and four bytes long, on both sides of the separator.
    let wide = [
        #"{"type":"user","message":{"role":"user","content":"把重試改成指數退避"}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":"好，改在 upload.rb 裡 🐈‍⬛"}}"#,
        #"{"type":"user","message":{"role":"user","content":"謝謝"}}"#,
    ].joined(separator: "\n")
    let multibyte = Transcript.parse(wide)
    expect("multi-byte lines are read", multibyte.count, 3)
    expect("and come back whole", multibyte[0].text, "把重試改成指數退避")
    expect("emoji with a zero-width joiner survive the slice", multibyte[1].text,
           "好，改在 upload.rb 裡 🐈‍⬛")
    expect("and the newest is still last", multibyte[2].text, "謝謝")
    expect("stopping early does not cut a character in half",
           Transcript.parse(wide, limit: 1)[0].text, "謝謝")
}

group("versioned Clawdline notices") {
    let hostileTitle = #"Review </script><b>unsafe</b> & `markdown` \"quoted\" 🐈‍⬛"#
    let hostilePath = #"/tmp/.clawdline/12345678-1234-1234-1234-123456789abc/result.json"#
    let task = ClawdlineMessage.Task(id: "12345678-1234-1234-1234-123456789abc",
                                     title: hostileTitle)
    let made = ClawdlineMessage.Notice(
        event: .taskFinished(task: task, state: .timeout, audience: .parent,
                             resultPath: hostilePath, outstanding: 2,
                             claimsReleased: true, childMayStillWrite: true),
        body: "[clawdline] the task timed out — read \(hostilePath)"
    )
    let wire = ClawdlineMessage.encode(made)
    check("the encoded envelope is one terminal-safe line",
          !wire.contains("\n") && !wire.contains("\r"))
    check("the exact wrapper carries a readable fallback", wire.hasPrefix("<clawdline-notice>{")
          && wire.hasSuffix("}</clawdline-notice>") && wire.contains("[clawdline]"))
    check("the wire carries the literal result.json path",
          wire.contains(hostilePath) && !wire.contains(#"\/"#))
    expect("the readable result.json wire still round-trips exactly",
           ClawdlineMessage.decode(wire), made)

    // The reviewed round swapped `hostilePath` for a clean one so that the two checks above
    // could say something precise about slashes. That left `result_path` with no
    // special-character coverage at all, so the awkward path comes back beside the clean one
    // rather than in place of it: a path is a label the card prints, and the shell and markup
    // characters a real temporary directory can hold must survive the envelope untouched.
    let awkwardPath = #"/tmp/<task>&"odd"/result `x`.json"#
    let awkward = ClawdlineMessage.Notice(
        event: .taskFinished(task: task, state: .failure, audience: .root,
                             resultPath: awkwardPath, outstanding: 0,
                             claimsReleased: false, childMayStillWrite: false),
        body: "[clawdline] the task failed — read \(awkwardPath)"
    )
    let awkwardWire = ClawdlineMessage.encode(awkward)
    check("a result_path full of shell and markup characters is still one terminal-safe line",
          !awkwardWire.contains("\n") && !awkwardWire.contains("\r"))
    check("and its slashes and entities reach the wire unescaped",
          awkwardWire.contains(#"/tmp/<task>&"#) && !awkwardWire.contains(#"\/"#)
              && !awkwardWire.contains("&amp;") && !awkwardWire.contains("&#"))
    expect("and an awkward result_path round-trips to the same typed payload",
           ClawdlineMessage.decode(awkwardWire), awkward)
    let awkwardRow = RemoteServer.transcriptRows(
        [Transcript.Entry(kind: .notice, text: awkward.body, tool: nil, time: nil,
                          notice: awkward)])
    expect("and the HTTP card is handed the path exactly as it was given",
           (awkwardRow.first?["notice"] as? [String: Any])?["result_path"] as? String,
           awkwardPath)

    for field in ["claims_released", "child_may_still_write"] {
        for numeric in [0, 1] {
            let numericBoolean = wire.replacingOccurrences(
                of: "\"\(field)\":true", with: "\"\(field)\":\(numeric)")
            check("\(field) rejects numeric \(numeric) instead of a JSON boolean",
                  numericBoolean != wire && ClawdlineMessage.decode(numericBoolean) == nil)
        }
    }

    let unknown = wire.replacingOccurrences(of: #""version":2"#,
                                             with: #""version":99"#)
    check("an unknown version is not interpreted", ClawdlineMessage.decode(unknown) == nil)
    check("a quoted lookalike is not interpreted", ClawdlineMessage.decode("> " + wire) == nil)
    check("a partial wrapper is not interpreted",
          ClawdlineMessage.decode(String(wire.dropLast())) == nil)
    check("prose around a valid envelope is not interpreted",
          ClawdlineMessage.decode("quote:\n" + wire + "\nend quote") == nil)
    let extra = wire.replacingOccurrences(of: #""version":2"#,
                                          with: #""version":2,"action":"javascript:bad()""#)
    check("unknown or executable fields make the closed schema invalid",
          ClawdlineMessage.decode(extra) == nil)

    let claudeRow = try! JSONSerialization.data(withJSONObject: [
        "type": "user",
        "message": ["role": "user", "content": [["type": "text", "text": wire]]],
    ], options: [.sortedKeys])
    let claudeEntries = Transcript.parse(String(decoding: claudeRow, as: UTF8.self))
    expect("Claude normalizes an exact envelope to one entry", claudeEntries.count, 1)
    expect("Claude gives it a dedicated notice kind", claudeEntries.first?.kind, .notice)
    expect("Claude keeps the typed payload", claudeEntries.first?.notice, made)

    let quotedRow = try! JSONSerialization.data(withJSONObject: [
        "type": "user",
        "message": ["role": "user", "content": [["type": "text", "text": "> " + wire]]],
    ], options: [.sortedKeys])
    let quotedEntries = Transcript.parse(String(decoding: quotedRow, as: UTF8.self))
    expect("a Claude quoted lookalike stays visible as user text",
           quotedEntries.first?.kind, .user)
    check("fallback does not disappear on failed recognition",
          quotedEntries.first?.text.contains("<clawdline-notice>") == true)

    let severalClaudeBlocksRow = try! JSONSerialization.data(withJSONObject: [
        "type": "user",
        "message": ["role": "user", "content": [
            ["type": "text", "text": wire],
            ["type": "text", "text": "quoted beside the envelope"],
        ]],
    ], options: [.sortedKeys])
    let severalClaudeBlocks = Transcript.parse(
        String(decoding: severalClaudeBlocksRow, as: UTF8.self))
    expect("an envelope beside another Claude block stays a user message",
           severalClaudeBlocks.first?.kind, .user)

    let codexEntries = Codex.entries(ofItem: [
        "type": "UserMessage", "content": [["type": "text", "text": wire]],
    ], at: nil)
    expect("Codex gives the same envelope the same notice kind",
           codexEntries.first?.kind, .notice)
    expect("Codex keeps the same typed payload", codexEntries.first?.notice, made)

    let severalCodexBlocks = Codex.entries(ofItem: [
        "type": "UserMessage",
        "content": [["type": "text", "text": wire], ["type": "text", "text": "quoted"]],
    ], at: nil)
    expect("an envelope beside another Codex block stays a user message",
           severalCodexBlocks.first?.kind, .user)

    let rows = RemoteServer.transcriptRows(claudeEntries)
    expect("the HTTP transcript role is notice", rows.first?["role"] as? String, "notice")
    let payload = rows.first?["notice"] as? [String: Any]
    check("the HTTP row carries controlled semantic fields",
          payload?["kind"] as? String == "task_finished"
              && payload?["state"] as? String == "timeout"
              && (payload?["task"] as? [String: Any])?["title"] as? String == hostileTitle)
    check("the HTTP card payload carries no fallback markup or protocol machinery",
          payload?["body"] == nil && payload?["protocol"] == nil && payload?["action"] == nil)

    // This is deliberately literal rather than produced by today's encoder: version 1 rows are
    // already stored in real transcripts and version 2 must not strand them.
    let literalV1 = #"<clawdline-notice>{"audience":"root","body":"[clawdline] old task finished","child_may_still_write":false,"claims_released":false,"kind":"task_finished","outstanding":0,"protocol":"clawdline.notice","result_path":"/tmp/old/result.json","state":"success","task":{"id":"old-task","title":"Old task"},"version":1}</clawdline-notice>"#
    check("a literal version 1 wire still decodes after version 2 ships",
          ClawdlineMessage.decode(literalV1)?.body == "[clawdline] old task finished")
    let literalV1Overlap = #"<clawdline-notice>{"audience":"root","body":"[clawdline] old workspace overlap","kind":"workspace_overlap","overlaps":[{"path":"Sources/Old.swift","task":{"id":"other-old-task","title":"Other old task"}}],"protocol":"clawdline.notice","task":{"id":"old-task","title":"Old task"},"version":1}</clawdline-notice>"#
    check("a literal version 1 workspace-overlap wire still decodes after version 2 ships",
          ClawdlineMessage.decode(literalV1Overlap).map {
              guard $0.body == "[clawdline] old workspace overlap",
                    case let .workspaceOverlap(task, audience, overlaps) = $0.event
              else { return false }
              return task.id == "old-task" && audience == .root && overlaps.count == 1
                && overlaps[0].task.id == "other-old-task"
                && overlaps[0].path == "Sources/Old.swift"
          } == true)
}

group("versioned Clawdline session messages") {
    let source = ClawdlineSessionMessage.Source(
        id: "A0939BAC-569B-4B87-9DF4-DE493EC327EA",
        label: #"clawdline-fa </div> & \"quoted\""#,
        assistant: .claude)
    let made = ClawdlineSessionMessage.Message(
        source: source,
        body: "你那兩點我都收進去了。\n\n## 狀態\n\n`e23f626b` 還在跑。")
    let wire = ClawdlineSessionMessage.encode(made)
    let handwrittenReport =
        "[a0939bac clawdline-fa｜⚠ 共用樹現在編不過] `e23f626b` 還在跑。"

    check("a session message is one physical terminal line",
          !wire.contains("\n") && !wire.contains("\r"))
    check("the session-message wrapper is distinct from task notices",
          wire.hasPrefix("<clawdline-message>{")
            && wire.hasSuffix("}</clawdline-message>")
            && !wire.contains(ClawdlineMessage.opening))
    expect("a multiline report and hostile source label round-trip exactly",
           ClawdlineSessionMessage.decode(wire), made)

    let unknown = wire.replacingOccurrences(of: #""version":1"#,
                                              with: #""version":99"#)
    check("an unknown session-message version is not interpreted",
          unknown != wire && ClawdlineSessionMessage.decode(unknown) == nil)
    let extra = wire.replacingOccurrences(of: #""version":1"#,
                                           with: #""version":1,"style":"danger""#)
    check("unknown presentation fields invalidate the closed session-message schema",
          extra != wire && ClawdlineSessionMessage.decode(extra) == nil)
    check("prose around a session-message envelope remains ordinary prose",
          ClawdlineSessionMessage.decode("forwarded: " + wire) == nil)

    let prefixed = Transcript.parse(noticeUserRow(
        handwrittenReport, at: "2026-08-28T06:00:00.000Z"))
    check("a hand-written cross-session sender prefix remains an ordinary user turn",
          prefixed.count == 1 && prefixed[0].kind == .user
            && prefixed[0].text == handwrittenReport && prefixed[0].source == nil)

    let claude = Transcript.parse(noticeUserRow(wire, at: "2026-08-28T06:00:00.000Z"))
    check("Claude gives an exact Clawdline relay its own message role",
          claude.count == 1 && claude[0].kind == .message
            && claude[0].text == made.body
            && claude[0].source == source.label
            && claude[0].sourceAssistant == .claude)
    let queuedRow = try! JSONSerialization.data(withJSONObject: [
        "type": "queue-operation", "operation": "enqueue", "content": wire,
        "timestamp": "2026-08-28T06:00:00.000Z",
    ])
    let queued = Transcript.parse(String(decoding: queuedRow, as: UTF8.self))
    check("a relay queued behind a busy Claude keeps the message role",
          queued.count == 1 && queued[0].kind == .message)

    let codex = Codex.entries(ofItem: [
        "type": "UserMessage", "content": [["type": "text", "text": wire]],
    ], at: nil)
    check("Codex gives the same relay the same message role and source",
          codex.count == 1 && codex[0].kind == .message
            && codex[0].source == source.label
            && codex[0].sourceAssistant == .claude)

    let row = RemoteServer.transcriptRows(claude).first
    check("HTTP names a relayed session message without calling it the user",
          row?["role"] as? String == "message"
            && row?["source"] as? String == source.label
            && row?["sourceAssistant"] as? String == "claude"
            && row?["notice"] == nil)
    check("a relayed session turn counts as external input for transcript ownership",
          Transcript.containsUserTurn(noticeUserRow(wire,
                                      at: "2026-08-28T06:00:00.000Z"), assistant: .claude))

    let first = TargetSession(backend: .iterm, id: "TERMINAL-A", name: "source A",
                              tty: "/dev/ttys070", windowIndex: 0, tabIndex: 0,
                              assistant: .claude, cwd: "/repo")
    let second = TargetSession(backend: .tmux, id: "%terminal-b", name: "source B",
                               tty: "/dev/ttys071", windowIndex: 0, tabIndex: 1,
                               assistant: .codex, cwd: "/repo")
    let sessions = [first, second]
    let conversations = ["TERMINAL-A": "conversation-a", "%terminal-b": "conversation-b"]
    let resolve: (String) -> TargetSession? = { id in
        RemoteServer.sessionMessageSource(withID: id, among: sessions) {
            conversations[$0.id]
        }
    }
    expect("a relay source resolves by exact terminal id", resolve("TERMINAL-A")?.id,
           "TERMINAL-A")
    expect("a relay source resolves by exact process-bound conversation id",
           resolve("conversation-b")?.id, "%terminal-b")
    check("a title or id prefix cannot impersonate a relay source",
          resolve("source A") == nil && resolve("TERMINAL") == nil)
    check("an ambiguous conversation id fails closed",
          RemoteServer.sessionMessageSource(withID: "same", among: sessions) { _ in "same" }
            == nil)

    let image = SessionImageArtifact(
        id: "11111111-2222-4333-8444-555555555555", mediaType: "image/png",
        byteCount: 73, width: 3, height: 2, expiresAt: 1_788_876_806)
    let pictured = ClawdlineSessionMessage.Message(
        source: source, body: "這是剛才的畫面。", artifacts: [image])
    let v2 = ClawdlineSessionMessage.encode(pictured)
    check("an image message advances only that wire to version 2",
          v2.contains(#""version":2"#) && !wire.contains(#""version":2"#))
    expect("a version 2 image reference round-trips without bytes or a URL",
           ClawdlineSessionMessage.decode(v2), pictured)
    check("the version 2 terminal envelope carries metadata but no retrievable address",
          v2.contains(#""artifacts""#) && !v2.contains("data:")
            && !v2.contains("http:") && !v2.contains("https:"))

    let extraArtifactField = v2.replacingOccurrences(
        of: #""width":3"#, with: #""path":"/tmp/private.png","width":3"#)
    check("an extra field inside a version 2 artifact invalidates the whole envelope",
          extraArtifactField != v2
            && ClawdlineSessionMessage.decode(extraArtifactField) == nil)
    let unsupportedArtifact = v2.replacingOccurrences(of: "image/png", with: "image/svg+xml")
    check("a version 2 artifact has one closed media type",
          unsupportedArtifact != v2
            && ClawdlineSessionMessage.decode(unsupportedArtifact) == nil)

    let picturedEntry = Transcript.clawdlineSessionMessage(in: v2, at: nil)
    let picturedRow = picturedEntry.flatMap { RemoteServer.transcriptRows([$0]).first }
    check("HTTP carries typed artifact metadata on the attributed message row",
          picturedRow?["role"] as? String == "message"
            && (picturedRow?["artifacts"] as? [[String: Any]])?.count == 1
            && ((picturedRow?["artifacts"] as? [[String: Any]])?.first?["id"] as? String)
                == image.id
            && picturedRow?["path"] == nil && picturedRow?["url"] == nil)
}

group("session image artifacts are owned, bounded and expire explicitly") {
    let root = isolatedTestSessionImagesDirectory
        .appendingPathComponent(UUID().uuidString, isDirectory: true)
    let source = root.appendingPathComponent("misleading-name.txt")
    do {
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    } catch {
        check("the owned-artifact fixture directory can be created", false, "\(error)")
        return
    }
    guard let original = exactPixelPNG(width: 3, height: 2,
                                       rgba: (236, 72, 153, 255)) else {
        check("the owned-artifact fixture encodes exact 3x2 pixels", false)
        return
    }
    do {
        try original.write(to: source)
    } catch {
        check("the owned-artifact fixture can be written", false, "\(error)")
        return
    }

    let policy = SessionImageArtifactStore.Policy(
        ttl: 10, maxCount: 1, maxTotalBytes: 1 << 20,
        maxInputBytes: 1 << 20, maxEncodedBytes: 1 << 20,
        maxDimension: 100, maxPixels: 10_000, tombstoneTTL: 20,
        maxMetadataCount: 8, maxImagesPerMessage: 2)
    let store = SessionImageArtifactStore(directory: root, policy: policy)
    let now = Date(timeIntervalSince1970: 1_800_000_000)
    let first: SessionImageArtifactStore.Stored
    do {
        guard let imported = try store.importPaths([source.path], now: now).first else {
            check("the exact-pixel fixture imports one artifact", false)
            return
        }
        first = imported
    } catch {
        check("the exact-pixel fixture imports one artifact", false, "\(error)")
        return
    }
    expect("an imported image is detected from decoded bytes, not its extension",
           first.artifact.mediaType, "image/png")
    expect("decoded dimensions become stable client metadata", first.artifact.width, 3)
    expect("decoded height becomes stable client metadata", first.artifact.height, 2)
    expect("expiry is an absolute instant fixed when the artifact is accepted",
           first.artifact.expiresAt, 1_800_000_010)
    check("the stored file is an owned re-encoded PNG",
          first.file.deletingLastPathComponent().standardizedFileURL
            == root.standardizedFileURL
            && (try? Data(contentsOf: first.file))?.starts(with: [0x89, 0x50, 0x4e, 0x47]) == true)

    switch store.lookup(id: first.artifact.id, now: now.addingTimeInterval(1)) {
    case .live(let artifact, let data):
        check("a live lookup returns the same typed metadata and bytes",
              artifact == first.artifact && data == (try? Data(contentsOf: first.file)))
    default:
        check("a live lookup returns the same typed metadata and bytes", false)
    }

    let second: SessionImageArtifactStore.Stored
    do {
        guard let imported = try store.importPaths(
            [source.path], now: now.addingTimeInterval(2)).first else {
            check("a second exact-pixel fixture imports for pruning", false)
            return
        }
        second = imported
    } catch {
        check("a second exact-pixel fixture imports for pruning", false, "\(error)")
        return
    }
    check("count pruning deletes only an owned older artifact",
          !FileManager.default.fileExists(atPath: first.file.path)
            && FileManager.default.fileExists(atPath: second.file.path)
            && FileManager.default.fileExists(atPath: source.path))
    if case .expired = store.lookup(id: first.artifact.id, now: now.addingTimeInterval(2)) {
        check("a pruned known id remains a typed tombstone", true)
    } else {
        check("a pruned known id remains a typed tombstone", false)
    }
    if case .expired = store.lookup(id: second.artifact.id, now: now.addingTimeInterval(12)) {
        check("TTL expiry is deterministic through the supplied clock", true)
    } else {
        check("TTL expiry is deterministic through the supplied clock", false)
    }
    if case .missing = store.lookup(
        id: "99999999-8888-4777-8666-555555555555", now: now) {
        check("an id never owned by the store is distinct from expiry", true)
    } else {
        check("an id never owned by the store is distinct from expiry", false)
    }

    do {
        _ = try store.importPaths(["https://example.test/picture.png"], now: now)
        check("remote and relative image paths are rejected", false)
    } catch let refusal as SessionImageArtifactStore.Refusal {
        expect("remote and relative image paths are rejected", refusal.code,
               "invalid_image_path")
    } catch {
        check("remote and relative image paths are rejected", false)
    }
    let fake = root.appendingPathComponent("fake.png")
    do {
        try Data("not an image".utf8).write(to: fake)
    } catch {
        check("the unsupported-image fixture can be written", false, "\(error)")
        return
    }
    do {
        _ = try store.importPaths([fake.path], now: now)
        check("unsupported bytes are rejected before storage", false)
    } catch let refusal as SessionImageArtifactStore.Refusal {
        expect("unsupported bytes are rejected before storage", refusal.code,
               "unsupported_image")
    } catch {
        check("unsupported bytes are rejected before storage", false)
    }
    let tightPolicy = SessionImageArtifactStore.Policy(
        ttl: 10, maxCount: 2, maxTotalBytes: 1 << 20,
        maxInputBytes: max(1, original.count - 1), maxEncodedBytes: 1 << 20,
        maxDimension: 100, maxPixels: 10_000, tombstoneTTL: 20,
        maxMetadataCount: 8, maxImagesPerMessage: 2)
    do {
        _ = try SessionImageArtifactStore(directory: root, policy: tightPolicy)
            .importPaths([source.path], now: now)
        check("oversized source bytes are refused before decode or storage", false)
    } catch let refusal as SessionImageArtifactStore.Refusal {
        expect("oversized source bytes are refused before decode or storage", refusal.code,
               "image_too_large")
    } catch {
        check("oversized source bytes are refused before decode or storage", false)
    }
}

group("native session images render live thumbnails and explicit expiry") {
    let root = isolatedTestSessionImagesDirectory
        .appendingPathComponent(UUID().uuidString, isDirectory: true)
    do {
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    } catch {
        check("the native-image fixture directory can be created", false, "\(error)")
        return
    }
    let source = root.appendingPathComponent("source.png")
    guard let png = exactPixelPNG(width: 640, height: 360,
                                  rgba: (20, 184, 166, 255)) else {
        check("the native-image fixture encodes exact 640x360 pixels", false)
        return
    }
    do {
        try png.write(to: source)
    } catch {
        check("the native-image fixture can be written", false, "\(error)")
        return
    }

    let policy = SessionImageArtifactStore.Policy(
        ttl: 10, maxCount: 2, maxTotalBytes: 1 << 20,
        maxInputBytes: 1 << 20, maxEncodedBytes: 1 << 20,
        maxDimension: 1_000, maxPixels: 1_000_000, tombstoneTTL: 20,
        maxMetadataCount: 8, maxImagesPerMessage: 2)
    let store = SessionImageArtifactStore(directory: root, policy: policy)
    let now = Date(timeIntervalSince1970: 1_800_000_000)
    let artifact: SessionImageArtifact
    do {
        guard let imported = try store.importPaths([source.path], now: now).first else {
            check("the native exact-pixel fixture imports one artifact", false)
            return
        }
        artifact = imported.artifact
    } catch {
        check("the native exact-pixel fixture imports one artifact", false, "\(error)")
        return
    }
    let live = SessionImagePresentation.render(
        artifact, size: 12, store: store, now: now.addingTimeInterval(1))
    var attachment: NSTextAttachment?
    var previewLink: String?
    live.enumerateAttributes(in: NSRange(location: 0, length: live.length)) { attrs, _, _ in
        attachment = attachment ?? attrs[.attachment] as? NSTextAttachment
        previewLink = previewLink ?? (attrs[.link] as? String)
    }
    check("a live native artifact becomes an actual image attachment", attachment != nil)
    check("the native thumbnail stays inside its documented bounds",
          (attachment?.bounds.width ?? 1_000) <= SessionImagePresentation.maximumThumbnail.width
            && (attachment?.bounds.height ?? 1_000)
                <= SessionImagePresentation.maximumThumbnail.height)
    expect("the thumbnail's private link routes only by opaque artifact id",
           previewLink.flatMap(SessionImagePresentation.artifactID), artifact.id)
    check("native presentation never prints an id, path, URL or image bytes",
          !live.string.contains(artifact.id) && !live.string.contains(source.path)
            && !live.string.contains("http") && !live.string.contains("data:"))

    let transcript = Transcript.render([
        .init(kind: .message, text: "A retained caption", tool: nil, time: nil,
              artifacts: [artifact]),
    ], size: 12, mono: NSFont.monospacedSystemFont(ofSize: 11, weight: .regular),
       imageStore: store, now: now.addingTimeInterval(1))
    var transcriptAttachmentCount = 0
    transcript.enumerateAttribute(.attachment,
                                  in: NSRange(location: 0, length: transcript.length)) {
        value, _, _ in
        if value is NSTextAttachment { transcriptAttachmentCount += 1 }
    }
    check("the native transcript keeps text and appends one image thumbnail",
          transcript.string.contains("A retained caption") && transcriptAttachmentCount == 1)

    var metadataChecks = 0
    var byteReads = 0
    let observedStore = SessionImageArtifactStore(
        directory: root, policy: policy,
        accessObserver: .init(
            didCheckMetadata: { metadataChecks += 1 },
            didReadBytes: { byteReads += 1 }))
    check("cached-image liveness stays on metadata and file existence",
          SessionImagePresentation.cacheIsCurrent(
            live, store: observedStore, now: now.addingTimeInterval(1)))
    expect("one cached image performs one metadata liveness check", metadataChecks, 1)
    expect("cache liveness reads no image bytes", byteReads, 0)
    _ = observedStore.lookup(id: artifact.id, now: now.addingTimeInterval(1))
    expect("a full lookup still performs its metadata check", metadataChecks, 2)
    expect("a full lookup is distinguishable by one byte read", byteReads, 1)

    let expired = SessionImagePresentation.render(
        artifact, size: 12, store: store, now: now.addingTimeInterval(10))
    check("past expires_at is a localized visible tile rather than a broken attachment",
          expired.string.contains(L.t.imageExpired)
            && expired.attribute(.attachment, at: 0, effectiveRange: nil) == nil)
    let missingReference = SessionImageArtifact(
        id: "99999999-8888-4777-8666-555555555555", mediaType: "image/png",
        byteCount: 73, width: 640, height: 360, expiresAt: 1_800_000_100)
    let missing = SessionImagePresentation.render(
        missingReference, size: 12, store: store, now: now)
    check("a native missing lookup uses the same unmistakable expired tile",
          missing.string.contains(L.t.imageExpired))
    check("English and Traditional Chinese name expiry explicitly",
          English().imageExpired == "Image expired"
            && TraditionalChinese().imageExpired == "圖片已過期")
    let webFallback = (try? String(contentsOfFile: "Resources/web/app/js/core/i18n.js")) ?? ""
    let webServer = (try? String(contentsOfFile: "Sources/RemoteServer.swift")) ?? ""
    for key in ["webImageExpired", "webImagePreview", "webImageClose"] {
        check("the browser fallback and /v1/strings both carry \(key)",
              webFallback.contains("\(key):") && webServer.contains("\"\(key)\":"))
    }
}

group("session image artifact HTTP retrieval is typed and authenticated") {
    let sourceSession = TargetSession(
        backend: .iterm, id: "IMAGE-SOURCE", name: "image source",
        tty: "/dev/ttys080", windowIndex: 0, tabIndex: 0,
        assistant: .claude, cwd: "/repo")
    let targetSession = TargetSession(
        backend: .tmux, id: "%image-target", name: "image target",
        tty: "/dev/ttys081", windowIndex: 0, tabIndex: 1,
        assistant: .codex, cwd: "/repo")
    RemoteServer.sessionPayloadForTesting = (
        [sourceSession, targetSession], [sourceSession.id: .idle, targetSession.id: .idle])
    var sent: [String] = []
    RemoteServer.terminalSendForTesting = { text, _ in sent.append(text); return nil }
    var clock = Date(timeIntervalSince1970: 1_800_100_000)
    RemoteServer.imageArtifactNowForTesting = { clock }
    defer {
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.terminalSendForTesting = nil
        RemoteServer.imageArtifactNowForTesting = nil
    }

    let input = isolatedTestSessionImagesDirectory.appendingPathComponent("route-source.png")
    guard let png = exactPixelPNG(width: 4, height: 3,
                                  rgba: (59, 130, 246, 255)) else {
        check("the HTTP fixture encodes exact 4x3 pixels", false)
        return
    }
    do {
        try png.write(to: input)
    } catch {
        check("the HTTP exact-pixel fixture can be written", false, "\(error)")
        return
    }

    var headers = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken(),
                   "Idempotency-Key": UUID().uuidString]
    let object: [String: Any] = [
        "from_session": sourceSession.id, "to_session": targetSession.id,
        "text": "請看這張圖。", "images": [["path": input.path]],
    ]
    guard let objectData = try? JSONSerialization.data(withJSONObject: object),
          let body = String(data: objectData, encoding: .utf8) else {
        check("the HTTP image-message fixture serializes", false)
        return
    }
    let accepted = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/messages", headers: headers, body: body))
    expect("a valid local image message is accepted once", accepted.status, 200)
    let acceptedBody = (try? JSONSerialization.jsonObject(with: accepted.body)) as? [String: Any]
    let artifact = (acceptedBody?["artifacts"] as? [[String: Any]])?.first
    let artifactID = artifact?["id"] as? String ?? ""
    check("the relay response exposes only stable artifact metadata",
          !artifactID.isEmpty && artifact?["width"] as? Int == 4
            && artifact?["height"] as? Int == 3
            && artifact?["path"] == nil && artifact?["url"] == nil)
    let decoded = sent.first.flatMap(ClawdlineSessionMessage.decode)
    check("the target receives the attributed v2 envelope rather than a /send fallback",
          sent.count == 1 && decoded?.artifacts.first?.id == artifactID)

    let phone = RemoteAuth.addDevice(name: "artifact reader", caps: [.read])
    defer { RemoteAuth.revoke(id: phone.id) }
    let readHeaders = ["Authorization": "Bearer \(phone.token)"]
    let unauthenticated = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/artifacts/images/\(artifactID)"))
    expect("an image artifact refuses a request with no token", unauthenticated.status, 401)
    expect("a missing image credential has the typed auth refusal",
           remoteErrorCode(unauthenticated), "unauthorized")
    let wrongToken = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/artifacts/images/\(artifactID)",
        headers: ["Authorization": "Bearer definitely-not-a-device-token"]))
    expect("an image artifact refuses a wrong token", wrongToken.status, 401)
    expect("a wrong image credential has the typed auth refusal",
           remoteErrorCode(wrongToken), "unauthorized")
    let live = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/artifacts/images/\(artifactID)", headers: readHeaders))
    expect("a read-capability token retrieves a live artifact", live.status, 200)
    check("live bytes have an exact type and private no-store policy",
          live.headers["Content-Type"] == "image/png"
            && live.headers["Cache-Control"] == "private, no-store"
            && live.body.starts(with: [0x89, 0x50, 0x4e, 0x47]))

    clock = clock.addingTimeInterval(SessionImageArtifactStore.productionPolicy.ttl + 1)
    let expired = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/artifacts/images/\(artifactID)", headers: readHeaders))
    expect("an expired artifact has HTTP gone semantics", expired.status, 410)
    expect("expiry is a typed client branch", remoteErrorCode(expired), "artifact_expired")
    let unknown = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/artifacts/images/99999999-8888-4777-8666-555555555555",
        headers: readHeaders))
    expect("an unknown artifact stays distinct from expiry", unknown.status, 404)
    expect("unknown ids have their own typed branch", remoteErrorCode(unknown),
           "artifact_not_found")

    headers["Idempotency-Key"] = UUID().uuidString
    var bad = object
    bad["images"] = [["path": input.path, "url": "https://example.test/leak.png"]]
    let sendsBefore = sent.count
    guard let badData = try? JSONSerialization.data(withJSONObject: bad),
          let badBody = String(data: badData, encoding: .utf8) else {
        check("the invalid image-input fixture serializes", false)
        return
    }
    let extra = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/messages", headers: headers, body: badBody))
    expect("extra image-input fields are rejected", extra.status, 400)
    check("request validation finishes before any terminal send", sent.count == sendsBefore)
}

group("version 2 file-wait and handoff notices") {
    let hostileRepository = #"/repo/<unsafe>&\"quoted\""#
    let hostilePaths = [#"Sources/<script>.swift"#, #"docs/a, b & `c`.md"#]
    let request = ClawdlineMessage.Notice(
        event: .fileWaitRequest(
            waitID: "wait-<one>", repository: hostileRepository, paths: hostilePaths,
            waiterSessionID: #"waiter-\"one\""#, reason: "Need <review> & `merge`\nnow",
            releaseCondition: "Commit \"safe\" & release\nafter review"),
        body: "[Clawdline file-wait] Repo: \(hostileRepository). Exact paths: "
            + hostilePaths.joined(separator: ", ") + ".\nKeep every operational detail.")
    let release = ClawdlineMessage.Notice(
        event: .fileWaitRelease(
            waitID: "wait-<one>", repository: hostileRepository, paths: hostilePaths,
            commit: #"abc123<&\""#, note: "Owner says <done> & `safe`\nwith care"),
        body: "[Clawdline file-wait release] Repo: \(hostileRepository). Exact paths: "
            + hostilePaths.joined(separator: ", ")
            + ". Landed/released in commit abc123. Note: keep care. "
            + "Re-check HEAD, status and diff before editing or integrating.")
    let pickedUp = ClawdlineMessage.Notice(
        event: .handoffReceipt(
            handoffID: "7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431",
            title: #"Cloud <plan> & \"ship\""#, assistant: .codex,
            projectDir: #"/tmp/<repo>&\"quoted\""#, state: .pickedUp),
        body: "[clawdline] handoff 7c1e9b02 picked up by codex")
    let firstLineFailed = ClawdlineMessage.Notice(
        event: .handoffReceipt(
            handoffID: "7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431", title: nil,
            assistant: .claude, projectDir: "/tmp/repo", state: .firstLineFailed),
        body: "[clawdline] handoff 7c1e9b02 opened a tab but the first line never landed "
            + "— type it in by hand")
    let notices = [request, release, pickedUp, firstLineFailed]

    for (index, notice) in notices.enumerated() {
        let wire = ClawdlineMessage.encode(notice)
        check("v2 notice \(index) stays one physical terminal line",
              !wire.contains("\n") && !wire.contains("\r"))
        expect("v2 notice \(index) round-trips hostile typed fields exactly",
               ClawdlineMessage.decode(wire), notice)

        let claude = Transcript.parse(noticeUserRow(wire, at: "2026-08-26T10:00:00.000Z"))
        check("Claude normalizes v2 notice \(index) to notice without losing the payload",
              claude.count == 1 && claude[0].kind == .notice && claude[0].notice == notice)
        let codex = Codex.entries(ofItem: [
            "type": "UserMessage", "content": [["type": "text", "text": wire]],
        ], at: nil)
        check("Codex normalizes v2 notice \(index) to notice without losing the payload",
              codex.count == 1 && codex[0].kind == .notice && codex[0].notice == notice)

        let row = RemoteServer.transcriptRows(claude).first
        check("HTTP serializes v2 notice \(index) as a controlled notice row",
              row?["role"] as? String == "notice"
                && (row?["notice"] as? [String: Any])?["kind"] as? String != nil
                && (row?["notice"] as? [String: Any])?["body"] == nil)
    }

    let requestWire = ClawdlineMessage.encode(request)
    let unknownKind = requestWire.replacingOccurrences(
        of: #""kind":"file_wait_request""#, with: #""kind":"future_wait""#)
    check("a v2 unknown kind falls back instead of being partly interpreted",
          unknownKind != requestWire && ClawdlineMessage.decode(unknownKind) == nil)
    let malformed = String(requestWire.dropLast())
    check("a malformed v2 wrapper falls back", ClawdlineMessage.decode(malformed) == nil)
    check("a quoted v2 lookalike falls back", ClawdlineMessage.decode("> " + requestWire) == nil)
    let quoted = Transcript.parse(noticeUserRow("> " + requestWire,
                                                at: "2026-08-26T10:00:00.000Z"))
    check("a failed v2 recognition keeps every visible byte as an ordinary user row",
          quoted.first?.kind == .user && quoted.first?.text.contains(requestWire) == true)

    let peerAndNotices = ([queuedPeer("owner speaking", at: "2026-08-26T10:00:00.000Z")]
        + notices.enumerated().map { index, notice in
            noticeUserRow(ClawdlineMessage.encode(notice),
                          at: "2026-08-26T10:00:0\(index + 1).000Z")
        }).joined(separator: "\n")
    let coexist = Transcript.parse(peerAndNotices, assistant: .claude)
    check("a peer and every v2 notice kind coexist without changing one another's roles",
          coexist.count == notices.count + 1 && coexist.first?.kind == .peer
            && coexist.dropFirst().allSatisfy { $0.kind == .notice })
}

group("orchestrator notices preserve the model-readable completion contract") {
    let id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    var task = Orchestrator.Task(id: id, state: .success, kind: "custom",
                                 title: "A <title> & review", assistant: .codex,
                                 projectDir: "/repo", timeoutMinutes: 30, created: Date(),
                                 secretHash: String(repeating: "0", count: 64))
    let parent = Orchestrator.taskFinishedNotice(for: task, audience: .parent, outstanding: 3)
    check("a parent fallback names result.json and outstanding siblings",
          parent?.body.contains("/tmp/.clawdline/\(id)/result.json") == true
              && parent?.body.contains("3 more of yours still running") == true)
    let stateContract: [(Orchestrator.State, Bool, ClawdlineMessage.TaskState?)] = [
        (.queued, false, nil), (.spawning, false, nil), (.briefed, false, nil),
        (.success, true, .success), (.failure, true, .failure),
        (.timeout, true, .timeout), (.cancelled, true, .cancelled),
        (.spawnFailed, true, .spawnFailed),
    ]
    for (source, terminal, expectedState) in stateContract {
        expect("terminal classification for \(source.rawValue)", source.isTerminal, terminal)
        expect("notice-state mapping for \(source.rawValue)",
               Orchestrator.noticeState(for: source), expectedState)
        task.state = source
        let notice = Orchestrator.taskFinishedNotice(for: task, audience: .root)
        check("notice presence follows terminal classification for \(source.rawValue)",
              (notice != nil) == terminal)
        if case let .taskFinished(_, actualState, _, _, _, _, _)? = notice?.event {
            expect("typed notice state for \(source.rawValue)", actualState, expectedState)
        }
    }
    task.state = .timeout
    task.claims = ["Sources"]
    let timeout = Orchestrator.taskFinishedNotice(for: task, audience: .root)
    check("timeout semantics carry the released-claim warning",
          timeout?.body.contains("claims released; child tab may still be writing") == true)
    if case let .taskFinished(_, state, _, _, _, released, mayWrite)? = timeout?.event {
        check("timeout state and warning are typed rather than parsed from prose",
              state == .timeout && released && mayWrite)
    } else {
        check("timeout event is typed", false)
    }
}

group("both completion notices still carry the untouched-claims reminder") {
    let id = "cafecafe-1111-2222-3333-444444444444"
    var task = Orchestrator.Task(id: id, state: .success, kind: "custom",
                                 title: "Wide claims", assistant: .claude,
                                 projectDir: "/repo", timeoutMinutes: 30, created: Date(),
                                 secretHash: String(repeating: "0", count: 64))
    task.claims = ["Sources", "docs", "Tests"]
    task.claimsDeclared = true
    task.untouchedClaims = ["docs", "Tests"]
    let reminder = Orchestrator.untouchedClaimsNotice(for: task)
    check("the fixture really has something to report", !reminder.isEmpty)

    for audience in [ClawdlineMessage.Audience.root, .parent] {
        let notice = Orchestrator.taskFinishedNotice(for: task, audience: audience,
                                                     outstanding: audience == .parent ? 2 : 0)
        check("the \(audience.rawValue) notice body keeps the untouched-claims reminder",
              notice?.body.contains(reminder) == true)
        // What the call site actually types into the terminal, not just what it composed.
        let wire = notice.map(ClawdlineMessage.encode) ?? ""
        check("and it survives the wire to the \(audience.rawValue)",
              ClawdlineMessage.decode(wire)?.body.contains("2 claimed path(s) never touched") == true
                  && ClawdlineMessage.decode(wire)?.body.contains("claim narrower next time") == true)
    }

    // The reminder follows the timeout warning's slot and precedes any root-owned landing hint,
    // so the two established orderings remain: root is "… — file — untouched", parent is
    // "… — file — siblings — untouched".
    let parent = Orchestrator.taskFinishedNotice(for: task, audience: .parent, outstanding: 2)
    check("the parent line still reads siblings-then-claims",
          parent?.body.contains("2 more of yours still running" + reminder) == true)

    var clean = task
    clean.untouchedClaims = []
    let quiet = Orchestrator.taskFinishedNotice(for: clean, audience: .root)
    check("a task that touched everything it claimed says nothing about claims",
          quiet?.body.contains("never touched") == false)

    // Everything above tests `taskFinishedNotice` as a pure function, and a pure function stays
    // green while the wire goes quiet. Pin the background postman's call site too: it chooses the
    // audience once, supplies the persisted notice id, and sends only the encoded envelope.
    let orchestratorSource = (try? String(contentsOfFile: "Sources/Orchestrator.swift",
                                          encoding: .utf8)) ?? ""
    let postman = orchestratorSource
        .components(separatedBy: "static func completionAttempt(taskID:")
        .dropFirst().first?.components(separatedBy: "private static func productionCompletionDelivery")
        .first ?? ""
    check("the completion postman body was located",
          postman.contains("let audience:") && postman.contains("deliver(task,"))
    check("the postman does not hand-build a completion line",
          !postman.contains("[clawdline]"))
    check("both audiences use the same persisted-notice composer",
          postman.contains("taskFinishedNotice(for: task, audience: audience")
              && postman.contains("noticeID: delivery.noticeID"))
    check("and the transport receives only the encoded semantic notice",
          postman.contains("deliver(task, ClawdlineMessage.encode(notice))"))
    let notifyRootBody = orchestratorSource
        .components(separatedBy: "private static func notifyRoot(_ task: Task) {")
        .dropFirst().first?.components(separatedBy: "\n    }\n").first ?? ""
    check("notifyRoot's body was located, so the checks below cannot pass on an empty string",
          notifyRootBody.contains("task.rootSessionId")
              && notifyRootBody.contains("deliverTerminalNotice("))
    check("neither call site hand-builds a completion line of its own",
          !notifyRootBody.contains("[clawdline]"))
    check("both call sites compose their notice through taskFinishedNotice",
          notifyRootBody.components(separatedBy: "taskFinishedNotice(for: task").count - 1 == 2)
    let typed = notifyRootBody.components(separatedBy: "let line = ").dropFirst()
        .map { $0.components(separatedBy: "\n").first ?? "" }
    check("and type nothing but what ClawdlineMessage.encode returned for it",
          typed.count == 2 && typed.allSatisfy { $0 == "ClawdlineMessage.encode(notice)" }
              && notifyRootBody.components(separatedBy:
                  "deliverTerminalNotice(line,").count - 1 == 2)
}

group("file-wait and handoff deliveries type only the versioned envelope") {
    let source = (try? String(contentsOfFile: "Sources/Orchestrator.swift",
                              encoding: .utf8)) ?? ""
    let settle = source.components(separatedBy: "static func settleHandoff(")
        .dropFirst().first?.components(separatedBy: "/// Watch a briefed child").first ?? ""
    check("the handoff settlement source was located",
          settle.contains("handoffReceipt(") && settle.contains("deliverTerminalNotice("))
    check("handoff settlement sends only the encoded semantic receipt",
          settle.contains("deliverTerminalNotice(ClawdlineMessage.encode(receipt), to: sender")
              && !settle.contains("deliverTerminalNotice(receipt, to: sender"))

    let receiptComposer = source.components(separatedBy: "static func handoffReceipt(")
        .dropFirst().first?.components(separatedBy: "private static func successfulHandoffReply")
        .first ?? ""
    let messageSource = (try? String(contentsOfFile: "Sources/ClawdlineMessage.swift",
                                     encoding: .utf8)) ?? ""
    check("handoff assistant mapping is compiler-exhaustive instead of force-unwrapped",
          receiptComposer.contains("HandoffAssistant(assistant)")
            && !receiptComposer.contains("rawValue: assistant.rawValue")
            && messageSource.contains("init(_ assistant: Assistant)")
            && messageSource.contains("switch assistant"))

    let waits = source.components(separatedBy: "// MARK: - Cross-session coordination waits")
        .dropFirst().first?.components(separatedBy: "// MARK:").first ?? ""
    check("the coordination delivery source was located",
          waits.contains("registerCoordinationWait(") && waits.contains("releaseCoordinationWait("))
    check("both coordination call sites send only what the notice encoder returned",
          waits.components(separatedBy: "let message = ClawdlineMessage.encode(notice)").count - 1 == 2
              && waits.components(separatedBy: "deliver(owner, message)").count - 1 == 1
              && waits.components(separatedBy: "deliver(waiter.sessionID, message)").count - 1 == 1)
    check("the old plain-text delivery prefixes live only inside typed notice composers",
          source.components(separatedBy: "[Clawdline file-wait] Repo:").count - 1 == 1
              && source.components(separatedBy: "[Clawdline file-wait release] Repo:").count - 1 == 1)

    let serverSource = (try? String(contentsOfFile: "Sources/RemoteServer.swift",
                                    encoding: .utf8)) ?? ""
    let registerRoute = serverSource
        .components(separatedBy: #"case ("POST", "/v1/orchestrator/waits"):"#)
        .dropFirst().first?.components(separatedBy: "\n        case (").first ?? ""
    let releaseRoute = serverSource
        .components(separatedBy: #"&& path.hasSuffix("/release"):"#)
        .dropFirst().first?.components(separatedBy: "\n        case (").first ?? ""
    check("the real wait routes and picker-readiness implementation were located",
          registerRoute.contains("Orchestrator.registerCoordinationWait(")
              && releaseRoute.contains("Orchestrator.releaseCoordinationWait(")
              && serverSource.contains("private func coordinationReadiness("))
    check("both wait routes hand the broker the real picker check",
          serverSource.components(separatedBy:
              "readiness: self.coordinationReadiness").count - 1 == 2)
    check("registration and release each wire that check at their production route",
          registerRoute.components(separatedBy:
              "readiness: self.coordinationReadiness").count - 1 == 1
              && releaseRoute.components(separatedBy:
                  "readiness: self.coordinationReadiness").count - 1 == 1)
}

group("the Web transcript has an inert Clawdline card") {
    let js = (try? String(contentsOfFile: "Resources/web/app/js/view/transcript.js",
                          encoding: .utf8)) ?? ""
    let css = (try? String(contentsOfFile: "Resources/web/app/css/transcript.css",
                           encoding: .utf8)) ?? ""
    let mock = (try? String(contentsOfFile: "Resources/web/app/js/net/mock.js",
                            encoding: .utf8)) ?? ""
    let fallback = (try? String(contentsOfFile: "Resources/web/app/js/core/i18n.js",
                                encoding: .utf8)) ?? ""
    let server = (try? String(contentsOfFile: "Sources/RemoteServer.swift",
                              encoding: .utf8)) ?? ""
    check("the notice role is routed to a dedicated renderer",
          js.contains(#"e.role === "notice""#) && js.contains("function noticeHTML"))
    let noticeRenderer = js.components(separatedBy: "function noticeHTML(e) {").dropFirst().first?
        .components(separatedBy: "\n}\n").first ?? ""
    check("task states, overlap and plurals use fixed translation keys",
          noticeRenderer.contains("T.webNoticeCompleted")
              && noticeRenderer.contains("T.webNoticeTimedOut")
              && noticeRenderer.contains("T.webNoticeWorkspaceOverlap")
              && noticeRenderer.contains("T.webNoticeOneSibling")
              && noticeRenderer.contains("fill("))
    check("the three new kinds and both handoff outcomes use fixed translation keys",
          noticeRenderer.contains("T.webNoticeFileWaitRequested")
              && noticeRenderer.contains("T.webNoticeFileWaitReleased")
              && noticeRenderer.contains("T.webNoticeHandoffPickedUp")
              && noticeRenderer.contains("T.webNoticeHandoffNeedsDelivery")
              && noticeRenderer.contains("T.webNoticeRecheckGit"))
    func renderEntry(_ entry: [String: Any]) -> (html: String, status: Int32) {
        guard let rendererStart = js.range(of: "function whoHTML(role, at) {") else {
            return ("", -1)
        }
        let renderer = String(js[rendererStart.lowerBound...]).replacingOccurrences(
            of: "export function entryHTML(", with: "function entryHTML(")
        func javascriptLiteral(_ value: Any) -> String {
            let data = try! JSONSerialization.data(withJSONObject: value,
                                                   options: [.fragmentsAllowed])
            return String(decoding: data, as: UTF8.self)
        }
        let script = """
        var WHO = { user: "you", assistant: "claude", peer: "Claude ↔", message: "Clawdline ↔", notice: "Clawdline", tool: "tool" };
        var S = { assistantIcons: false, expanded: {} };
        var T = new Proxy({}, { get: function (_, key) { return String(key); } });
        function byId() { return null; }
        function assistantLogo(value) { return "LOGO:" + esc(value); }
        function assistantName(value) { return String(value || "").toUpperCase(); }
        function clockOf(value) { return String(value || ""); }
        function fill(value) { return String(value || ""); }
        function foldKey() { return "test-patch"; }
        function esc(value) {
            return String(value == null ? "" : value)
                .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
                .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
        }
        function richText(value) { return "BODY:" + esc(value); }
        eval(\(javascriptLiteral(renderer)));
        process.stdout.write(entryHTML(\(javascriptLiteral(entry))));
        """
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["node"]
        let input = Pipe(), output = Pipe()
        process.standardInput = input
        process.standardOutput = output
        process.standardError = Pipe()
        do {
            try process.run()
            input.fileHandleForWriting.write(Data(script.utf8))
            try? input.fileHandleForWriting.close()
            let html = String(decoding: output.fileHandleForReading.readDataToEndOfFile(),
                              as: UTF8.self)
            process.waitUntilExit()
            return (html, process.terminationStatus)
        } catch {
            return ("", -1)
        }
    }
    func foldedDescription(_ names: [String]) -> (text: String, status: Int32) {
        guard let start = js.range(of: "function foldedRunDescription(names) {")?.lowerBound,
              let end = js.range(of: "\nfunction foldHTML", range: start..<js.endIndex)?.lowerBound
        else { return ("", -1) }
        let source = String(js[start..<end])
        let sourceData = try! JSONSerialization.data(withJSONObject: source,
                                                      options: [.fragmentsAllowed])
        let namesData = try! JSONSerialization.data(withJSONObject: names)
        let script = "eval(" + String(decoding: sourceData, as: UTF8.self) + ");"
            + "process.stdout.write(foldedRunDescription("
            + String(decoding: namesData, as: UTF8.self) + "));"
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["node"]
        let input = Pipe(), output = Pipe()
        process.standardInput = input
        process.standardOutput = output
        process.standardError = Pipe()
        do {
            try process.run()
            input.fileHandleForWriting.write(Data(script.utf8))
            try? input.fileHandleForWriting.close()
            let text = String(decoding: output.fileHandleForReading.readDataToEndOfFile(),
                              as: UTF8.self)
            process.waitUntilExit()
            return (text, process.terminationStatus)
        } catch {
            return ("", -1)
        }
    }
    let claudeRun = foldedDescription([
        "mcp__claude-in-chrome__tabs_context_mcp",
        "mcp__claude-in-chrome__browser_batch", "Bash", "Bash",
    ])
    expect("a Claude provider run reads like Claude Code's own compact summary",
           claudeRun.text, "Called claude-in-chrome, ran 2 shell commands")
    expect("the compact run summary executes successfully", claudeRun.status, 0)
    let mismatched = renderEntry([
        "role": "notice", "text": "visible row text from the older server",
        "notice": ["kind": "task_finished", "state": "success"],
    ])
    check("a task-kind notice without a task falls back without hiding its visible text",
          mismatched.status == 0
            && mismatched.html.contains("BODY:visible row text from the older server")
            && mismatched.html.contains(#"data-role="assistant""#)
            && !mismatched.html.contains("clawdline-notice"))
    let relayed = renderEntry([
        "role": "message", "text": "## status & detail", "source": "<clawdline-fa>",
        "sourceAssistant": "claude",
    ])
    check("the real message renderer produces an escaped, Markdown-capable Clawdline card",
          relayed.status == 0
            && relayed.html.contains(#"data-role="message""#)
            && relayed.html.contains("&lt;clawdline-fa&gt;")
            && relayed.html.contains("LOGO:claude")
            && relayed.html.contains("BODY:## status &amp; detail")
            && !relayed.html.contains("<clawdline-fa>"))
    let edited = renderEntry([
        "role": "tool", "tool": "edit", "text": "Thing.swift, Added.swift",
        "fileChanges": [
            ["path": "/Users/me/a/Thing.swift", "kind": "update",
             "unifiedDiff": "@@ -7,2 +7,2 @@ function demo()\n-const old = '<unsafe>';\n+const fresh = '& ready';\n keep();\n"],
            ["path": "/Users/me/a/Added.swift", "kind": "add",
             "content": "let first = true\nlet second = false\n"],
        ],
    ])
    check("a Codex edit renders as an open, collapsible patch",
          edited.status == 0
            && edited.html.contains(#"data-role="patch""#)
            && edited.html.contains(#"aria-expanded="true""#)
            && edited.html.contains("Thing.swift"))
    check("deleted and added lines receive distinct semantic rows",
          edited.html.contains(#"class="diff-line del""#)
            && edited.html.contains(#"class="diff-line add""#)
            && edited.html.contains(#"class="diff-line context""#))
    check("diff code is escaped and never becomes transcript markup",
          edited.html.contains("const old = &#39;&lt;unsafe&gt;&#39;;")
            && edited.html.contains("const fresh = &#39;&amp; ready&#39;;")
            && !edited.html.contains("<unsafe>"))
    check("hunk counters and new-file lines are visible",
          edited.html.contains(#"<span class="old">7</span>"#)
            && edited.html.contains(#"<span class="new">7</span>"#)
            && edited.html.contains(#"data-kind="add""#)
            && edited.html.contains("let second = false"))
    let written = renderEntry([
        "role": "tool", "tool": "Write", "text": "/tmp/task/artifacts/format-sample.md",
        "fileChanges": [["path": "/tmp/task/artifacts/format-sample.md", "kind": "write",
                         "content": "cwd: /work\nhead: <unsafe>\ntools: Bash, Write\n"]],
    ])
    check("a Claude Write uses the same open patch card without losing its tool name",
          written.status == 0 && written.html.contains(#"data-role="patch""#)
            && written.html.contains("Write") && written.html.contains("format-sample.md")
            && written.html.contains(#"data-kind="write""#))
    check("Claude Write content is escaped and rendered as added lines",
          written.html.contains(#"class="diff-line add""#)
            && written.html.contains("head: &lt;unsafe&gt;")
            && !written.html.contains("<unsafe>"))
    let planned = renderEntry([
        "role": "tool", "tool": "plan", "text": "Updated Plan",
        "plan": [
            ["step": "Inspect <unsafe>", "status": "completed"],
            ["step": "Implement cards", "status": "inProgress"],
            ["step": "Verify", "status": "pending"],
        ],
    ])
    check("an Updated Plan renders as a dedicated checklist card",
          planned.status == 0 && planned.html.contains(#"data-role="plan""#)
            && planned.html.contains("Updated Plan") && planned.html.contains("Implement cards"))
    check("all three plan states have distinct semantic rows",
          planned.html.contains(#"data-status="completed""#)
            && planned.html.contains(#"data-status="inProgress""#)
            && planned.html.contains(#"data-status="pending""#))
    check("plan steps are escaped instead of becoming markup",
          planned.html.contains("Inspect &lt;unsafe&gt;") && !planned.html.contains("<unsafe>"))
    let called = renderEntry([
        "role": "tool", "tool": "browser.connect", "text": "Connect preview",
        "activity": ["kind": "called", "title": "Connect <preview>",
                     "status": "completed", "durationMs": 1250,
                     "result": "Connected & ready\npage two", "actions": []],
    ])
    check("a Called activity renders title, status, duration and result",
          called.status == 0 && called.html.contains(#"data-role="called""#)
            && called.html.contains("Connect &lt;preview&gt;")
            && called.html.contains("completed") && called.html.contains("1.25s")
            && called.html.contains("Connected &amp; ready") && called.html.contains("page two"))
    let explored = renderEntry([
        "role": "tool", "tool": "shell", "text": "Explore files",
        "activity": ["kind": "explored", "status": "completed", "actions": [
            ["kind": "search", "query": "title <tag>", "path": "Sources"],
            ["kind": "read", "name": "Codex.swift", "path": "Sources/Codex.swift"],
        ]],
    ])
    check("an Explored activity renders typed search and read actions",
          explored.status == 0 && explored.html.contains(#"data-role="explored""#)
            && explored.html.contains("Search") && explored.html.contains("Read")
            && explored.html.contains("Codex.swift") && explored.html.contains("Sources"))
    check("activity fields are escaped and never become transcript markup",
          explored.html.contains("title &lt;tag&gt;") && !explored.html.contains("<tag>"))
    let repeatedExploration = renderEntry([
        "role": "tool", "tool": "shell", "text": "Explore files",
        "activity": ["kind": "explored", "status": "completed", "actions": [
            ["kind": "read", "name": "Settings.swift", "path": "Sources/Settings.swift"],
            ["kind": "read", "name": "Settings.swift", "path": "Sources/Settings.swift"],
            ["kind": "search", "query": "Settings", "path": "Sources"],
            ["kind": "read", "name": "Settings.swift", "path": "Sources/Settings.swift"],
        ]],
    ])
    let repeatedRows = repeatedExploration.html.components(separatedBy: "<li>").count - 1
    expect("only adjacent duplicate activity rows are collapsed", repeatedRows, 3)
    expect("a repeated activity after a different action remains visible",
           repeatedExploration.html.components(separatedBy: "Settings.swift").count - 1, 4)
    check("task state lookup rejects inherited object properties and keeps a generic title",
          noticeRenderer.contains("Object.prototype.hasOwnProperty.call(states, n.state)")
              && noticeRenderer.contains("var title = T.webNoticeFinished"))
    check("notice data and translated copy are escaped",
          noticeRenderer.contains("esc(identity)") && noticeRenderer.contains("esc(path)")
              && noticeRenderer.contains("esc(title)")
              && noticeRenderer.contains("esc(siblings)")
              && noticeRenderer.contains("esc(T.webNoticeClaimsReleased)"))
    check("every payload field printed by a new card is escaped as plain text",
          noticeRenderer.contains("esc(n.reason)")
              && noticeRenderer.contains("esc(n.release_condition)")
              && noticeRenderer.contains("esc(n.commit)")
              && noticeRenderer.contains("esc(n.note)")
              && noticeRenderer.contains("esc(n.project_dir)"))
    check("the renderer has no payload-controlled rich text, link, action, style or copy key",
          !noticeRenderer.contains("richText(") && !noticeRenderer.contains("inlineMd(")
              && !noticeRenderer.contains("<a") && !noticeRenderer.contains("<button")
              && !noticeRenderer.contains("href=") && !noticeRenderer.contains("style=")
              && !noticeRenderer.contains("n.action") && !noticeRenderer.contains("n.style")
              && !noticeRenderer.contains("n.class") && !noticeRenderer.contains("T[n."))
    check("the card is a distinct inert role",
          css.contains(#".entry[data-role="notice"]"#)
              && css.contains(".notice-card") && !css.contains(".notice-card button"))
    check("a relayed session message has a role and card distinct from user, peer and notice",
          js.contains(#"role === "message""#)
              && js.contains(#"data-role="message""#)
              && js.contains("message-card")
              && css.contains(#".entry[data-role="message"]"#)
              && css.contains(".message-card"))
    check("the message card escapes its source and renders only the validated assistant mark",
          js.contains("esc(messageSource)")
              && js.contains("assistantLogo(sourceAssistant)")
              && js.contains("assistantName(sourceAssistant)"))
    check("the mock transcript exercises the notice role and hostile title",
          mock.contains(#"role: "notice""#) && mock.contains("Review <unsafe> & finish"))
    // The one screen this pair of features exists to prove is a peer card and a notice card,
    // visually distinct, in the same transcript — so `?mock=1` has to be able to draw it. It
    // could not until this check existed: the fixture had the notice row and no peer row at all.
    let mockTranscript = mock.components(separatedBy: #"transcripts["A15E-77"] = ["#)
        .dropFirst().first?.components(separatedBy: "\n    ];").first ?? ""
    check("and shows a peer card beside a notice card in one mock transcript",
          mockTranscript.contains(#"role: "peer""#)
              && mockTranscript.contains(#"role: "notice""#)
              && mockTranscript.contains(#"source: "release-room""#))
    check("the mock transcript also reaches the Clawdline session-message card",
          mockTranscript.contains(#"role: "message""#)
              && mockTranscript.contains(#"source: "clawdline-fa""#)
              && mockTranscript.contains(#"sourceAssistant: "claude""#))
    check("the mock covers both notice kinds, so the overlap card is reachable too",
          mockTranscript.contains(#"kind: "task_finished""#)
              && mockTranscript.contains(#"kind: "workspace_overlap""#))
    check("the mock reaches every new card and both handoff outcomes",
          mockTranscript.contains(#"kind: "file_wait_request""#)
              && mockTranscript.contains(#"kind: "file_wait_release""#)
              && mockTranscript.components(separatedBy: #"kind: "handoff_receipt""#).count - 1 == 2
              && mockTranscript.contains(#"state: "picked_up""#)
              && mockTranscript.contains(#"state: "first_line_failed""#))
    let newCopyKeys = [
        "webNoticeFileWaitRequested", "webNoticeFileWaitReleased",
        "webNoticeHandoffPickedUp", "webNoticeHandoffNeedsDelivery", "webNoticeRecheckGit",
    ]
    for key in newCopyKeys {
        check("the browser fallback and /v1/strings both carry \(key)",
              fallback.contains("\(key):") && server.contains("\"\(key)\":"))
    }
    for (tag, copy) in L.catalog {
        let values = [copy.webNoticeFileWaitRequested, copy.webNoticeFileWaitReleased,
                      copy.webNoticeHandoffPickedUp, copy.webNoticeHandoffNeedsDelivery,
                      copy.webNoticeRecheckGit]
        check("every new notice string is present in \(tag)",
              values.allSatisfy { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty })
    }
}

group("transcript tool summaries") {
    expect("command wins", Transcript.summarise(input: ["description": "d", "command": "ls -l"]), "ls -l")
    expect("then a path", Transcript.summarise(input: ["description": "d", "file_path": "/tmp/a"]), "/tmp/a")
    expect("then a pattern", Transcript.summarise(input: ["pattern": "TODO"]), "TODO")
    expect("only the first line", Transcript.summarise(input: ["command": "one\ntwo"]), "one")
    expect("nothing usable is empty", Transcript.summarise(input: ["weird": 3]), "")
    expect("not a dictionary is empty", Transcript.summarise(input: "string"), "")
}

group("transcript titles") {
    expect("status glyph is stripped", Transcript.cleanTitle("✳ fix the thing"), "fix the thing")
    expect("job name is stripped", Transcript.cleanTitle("✳ fix the thing (python)"), "fix the thing")
    expect("a plain title is untouched", Transcript.cleanTitle("fix the thing"), "fix the thing")
    expect("works on CJK", Transcript.cleanTitle("◑ 將輸入框移到中上方 (node)"), "將輸入框移到中上方")
    expect("empty stays empty", Transcript.cleanTitle("   "), "")

    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-transcript-titles-\(UUID().uuidString)",
                                isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    func write(_ name: String, _ rows: [String]) -> URL {
        let url = dir.appendingPathComponent("\(name).jsonl")
        try! Data(rows.joined(separator: "\n").utf8).write(to: url)
        return url
    }

    let aiOnly = write("ai-only", [
        #"{"type":"system","aiTitle":"first automatic title"}"#,
        #"{"type":"system","aiTitle":"last automatic title"}"#,
    ])
    expect("the last ai title is used when there is no custom title",
           Transcript.title(ofTranscript: aiOnly), "last automatic title")

    let customOnly = write("custom-only", [
        #"{"type":"custom-title","customTitle":"first chosen title"}"#,
        #"{"type":"custom-title","customTitle":"last chosen title"}"#,
    ])
    expect("the last custom title is used", Transcript.title(ofTranscript: customOnly),
           "last chosen title")

    // `customTitle(ofTranscript:)` is what `Config` compares a local name's baseline against —
    // it must never inherit `title(ofTranscript:)`'s fallback to `aiTitle`, or an ordinary
    // automatic title would look exactly like a person having typed `/rename`.
    expect("no explicit rename means no custom title, even with an ai title to fall back to",
           Transcript.customTitle(ofTranscript: aiOnly), nil)
    expect("an explicit rename is reported on its own", Transcript.customTitle(ofTranscript: customOnly),
           "last chosen title")

    let both = write("both", [
        #"{"type":"custom-title","customTitle":"修正瀏覽器問答"}"#,
        String(repeating: "x", count: 1_024),
        #"{"type":"system","aiTitle":"later automatic title"}"#,
    ])
    expect("an explicit title before the tail wins over a later ai title",
           Transcript.title(ofTranscript: both, tailBytes: 128), "修正瀏覽器問答")
}

group("transcript project directory") {
    let dir = Transcript.projectDirectory(forCwd: "/Users/me/code/atrium")
    expect("separators become dashes", dir.lastPathComponent, "-Users-me-code-atrium")
    check("under ~/.claude/projects", dir.path.contains("/.claude/projects/"))

    // **Every character that is not a letter or a digit**, not just the separators. Replacing `/`
    // alone is right until a path has a space, a dot or an underscore in it, and then the lookup
    // goes to a folder that does not exist — silently, because a missing transcript is a normal
    // state. These are the three that were wrong.
    expect("an underscore is a dash too",
           Transcript.projectDirectory(forCwd: "/a/two_words").lastPathComponent, "-a-two-words")
    expect("so is a dot",
           Transcript.projectDirectory(forCwd: "/a/v1.2").lastPathComponent, "-a-v1-2")
    expect("so is a space",
           Transcript.projectDirectory(forCwd: "/a/two words").lastPathComponent, "-a-two-words")

    // One rule, one implementation — the two used to be written out separately and would have
    // agreed only until somebody edited one of them.
    for path in ["/Users/me/code/a_b", "/x/y.z", "/p q/r", "/plain/path"] {
        expect("it is the same rule StartPoints uses, for \(path)",
               Transcript.projectDirectory(forCwd: path).lastPathComponent,
               StartPoints.slug(of: path))
    }
}

group("a hook session id is an identity boundary") {
    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-transcript-identity-\(UUID().uuidString)",
                                isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    let other = dir.appendingPathComponent("somebody-else.jsonl")
    try! Data("{}\n".utf8).write(to: other)

    let spawnedAt = Date()
    let staleNote = HookBridge.Note(event: .sessionStart, tty: "ttys004",
                                    at: spawnedAt.addingTimeInterval(-60),
                                    session: "previous-session")
    let stale = dir.appendingPathComponent("previous-session.jsonl")
    try! Data("{}\n".utf8).write(to: stale)
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(-30)],
                          ofItemAtPath: stale.path)

    // Reusing a tty leaves the previous SessionStart note in HookBridge until Claude emits a
    // note for the new process. Its named transcript is still an identity boundary, but it is
    // the boundary of the previous session and its contents predate this spawn.
    check("a session id from an old note cannot name a pre-spawn transcript",
          Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: spawnedAt,
                            sessionID: staleNote.session) == nil)

    // SessionStart fires before Claude creates its own jsonl. The old fallback chose `other`
    // because it was the only recent file, making a new browser-started tab show an existing
    // conversation until its own transcript appeared.
    check("a not-yet-created exact transcript does not fall back to another session",
          Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: Date(),
                            sessionID: "brand-new") == nil)

    let exact = dir.appendingPathComponent("brand-new.jsonl")
    try! Data("{}\n".utf8).write(to: exact)
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(1)],
                          ofItemAtPath: exact.path)
    expect("the exact transcript appears as soon as Claude creates it",
           Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: spawnedAt,
                             sessionID: "brand-new"), exact)
}

group("a task marker is the fallback transcript identity") {
    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-transcript-marker-\(UUID().uuidString)",
                                isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    let spawnedAt = Date()
    let markerTaskID = "0f8fad5b-d9cb-469f-a165-70867728950e"
    let receipt = #"{"type":"user","message":{"role":"user","content":"You are a Clawdline CHILD agent for task "#
        + markerTaskID + #"."}}"#
    let somebodyElse = #"{"type":"user","message":{"role":"user","content":"You are a Clawdline CHILD agent for task 11111111-2222-3333-4444-555555555555."}}"#
    let sameTitle = #"{"customTitle":"Claude Code"}"#
    let stale = dir.appendingPathComponent("stale-own.jsonl")
    let sibling = dir.appendingPathComponent("sibling.jsonl")
    try! Data((receipt + "\n" + sameTitle + "\n").utf8).write(to: stale)
    try! Data((somebodyElse + "\n" + sameTitle + "\n").utf8).write(to: sibling)
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(-1)],
                          ofItemAtPath: stale.path)
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(1)],
                          ofItemAtPath: sibling.path)

    let acceptsMine: (URL) -> Bool = {
        Orchestrator.transcriptBelongsToTask($0, assistant: .claude, taskID: markerTaskID)
    }
    expect("a missing transcript is unknown rather than evidence for another task",
           Orchestrator.transcriptOwnership(dir.appendingPathComponent("missing.jsonl"),
                                            assistant: .claude, taskID: markerTaskID),
           .unavailable)
    let empty = dir.appendingPathComponent("empty.jsonl")
    try! Data().write(to: empty)
    expect("a zero-byte transcript is still awaiting its first turn",
           Orchestrator.transcriptOwnership(empty, assistant: .claude,
                                            taskID: markerTaskID), .unavailable)
    let starting = dir.appendingPathComponent("starting.jsonl")
    try! Data(#"{"type":"system","subtype":"startup"}"#.utf8).write(to: starting)
    expect("startup metadata without a user turn cannot disprove ownership",
           Orchestrator.transcriptOwnership(starting, assistant: .claude,
                                            taskID: markerTaskID), .unavailable)
    expect("a readable sibling is positive evidence that this is not its task",
           Orchestrator.transcriptOwnership(sibling, assistant: .claude,
                                            taskID: markerTaskID), .other)
    check("a pre-spawn file is excluded even when it carries the requested marker",
          Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: spawnedAt,
                            accepting: acceptsMine) == nil)

    let own = dir.appendingPathComponent("own.jsonl")
    try! Data((receipt + "\n" + sameTitle + "\n").utf8).write(to: own)
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(2)],
                          ofItemAtPath: own.path)
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(3)],
                          ofItemAtPath: sibling.path)
    let sharedBirth = spawnedAt.addingTimeInterval(0.5)
    try! fm.setAttributes([.creationDate: sharedBirth], ofItemAtPath: sibling.path)
    try! fm.setAttributes([.creationDate: sharedBirth], ofItemAtPath: own.path)
    expect("the creation-time fixture really puts both siblings in the same second",
           Int(Transcript.created(sibling).timeIntervalSince1970),
           Int(Transcript.created(own).timeIntervalSince1970))
    expect("a same-window sibling cannot win over the file with this task's marker",
           Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: spawnedAt,
                             accepting: acceptsMine), own)
    check("a later exact hook id cannot replace proven identity without the task marker",
          Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: spawnedAt,
                            sessionID: "sibling", accepting: acceptsMine) == nil)
    expect("an exact hook id carrying the marker is eligible to replace an earlier guess",
           Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: spawnedAt,
                             sessionID: "own", accepting: acceptsMine), own)
    var unverified = Orchestrator.Task(id: markerTaskID, state: .spawning, kind: "custom",
                                       title: "a task", assistant: .claude, projectDir: "/tmp",
                                       timeoutMinutes: 30, created: Date(),
                                       secretHash: String(repeating: "0", count: 64))
    unverified.childSessionId = "earlier-guess"; unverified.transcriptPath = sibling.path
    var processBacked = unverified
    processBacked.childPID = 100; processBacked.childProcStart = spawnedAt
    let unverifiedChanged = Orchestrator.adoptHookIdentity(sessionID: "own", in: &unverified)
    let processChanged = Orchestrator.adoptHookIdentity(sessionID: "own", in: &processBacked)
    // Removing the complete-process requirement must let the unverified half change; refusing
    // every correction must keep the process-backed half on its earlier provisional pair.
    check("only a process-backed hook can correct provisional identity",
          !unverifiedChanged
            && unverified.childSessionId == "earlier-guess"
            && unverified.transcriptPath == sibling.path
            && processChanged
            && processBacked.childSessionId == "own"
            && processBacked.transcriptPath == nil
            && !processBacked.transcriptProven)
    var pinned = processBacked
    pinned.state = .briefed; pinned.transcriptPath = own.path; pinned.transcriptProven = true
    // Removing the receipt guard must let the later hook replace this pinned pair.
    check("a later hook cannot replace receipt-pinned identity",
          !Orchestrator.adoptHookIdentity(sessionID: "sibling", in: &pinned)
            && pinned.childSessionId == "own"
            && pinned.transcriptPath == own.path
            && pinned.transcriptProven)
    expect("the creation-time fallback rejects a same-second sibling too",
           Transcript.locate(in: dir, tabTitle: "A title neither file has", startedAt: spawnedAt,
                             accepting: acceptsMine), own)

    let long = receipt + "\n" + (0..<150).map { index in
        "{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\","
            + "\"content\":\"later \(index)\"}}"
    }.joined(separator: "\n")
    try! Data((long + "\n").utf8).write(to: own)
    check("restoring identity searches past the receipt-sized tail",
          Orchestrator.transcriptBelongsToTask(own, assistant: .claude,
                                               taskID: markerTaskID))

    let newlyProven = somebodyElse + "\n" + receipt + "\n" + sameTitle + "\n"
    try! Data(newlyProven.utf8).write(to: sibling)
    check("a cached rejection is retried when the transcript grows",
          Orchestrator.transcriptBelongsToTask(sibling, assistant: .claude,
                                               taskID: markerTaskID))

    let beyondLimit = dir.appendingPathComponent("marker-beyond-limit.jsonl")
    var beyondLimitData = Data(repeating: 0x20, count: 1_048_577)
    beyondLimitData.append(Data(("\n" + receipt + "\n").utf8))
    try! beyondLimitData.write(to: beyondLimit)
    expect("a marker beyond the bounded scan leaves ownership unknown",
           Orchestrator.transcriptOwnership(beyondLimit, assistant: .claude,
                                            taskID: markerTaskID), .unavailable)

    let longSibling = dir.appendingPathComponent("long-sibling.jsonl")
    var longSiblingData = Data((somebodyElse + "\n").utf8)
    longSiblingData.append(Data(repeating: 0x20, count: 1_048_577))
    try! longSiblingData.write(to: longSibling)
    expect("a bounded first user turn disproves even an oversized sibling",
           Orchestrator.transcriptOwnership(longSibling, assistant: .claude,
                                            taskID: markerTaskID), .other)
}

group("transcript ownership repairs only identities it disproves") {
    let fm = FileManager.default
    let ownershipTaskID = "0f8fad5b-d9cb-469f-a165-70867728950e"
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-transcript-ownership-\(UUID().uuidString)",
                                isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    func task(assistant: Assistant, childSession: String?, transcript: URL) -> Orchestrator.Task {
        var made = Orchestrator.Task(id: ownershipTaskID, state: .briefed, kind: "custom",
                                     title: "a task",
                                     assistant: assistant, projectDir: "/tmp", timeoutMinutes: 30,
                                     created: Date(), secretHash: String(repeating: "0", count: 64))
        made.childSessionId = childSession
        made.transcriptPath = transcript.path
        return made
    }

    let sibling = dir.appendingPathComponent("sibling.jsonl")
    let siblingReceipt = #"{"type":"user","message":{"role":"user","content":"You are a Clawdline CHILD agent for task 11111111-2222-3333-4444-555555555555."}}"#
    try! Data((siblingReceipt + "\n").utf8).write(to: sibling)
    var preBriefing = Orchestrator.Task(
        id: ownershipTaskID, state: .spawning, kind: "custom", title: "a task",
        assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date(), secretHash: String(repeating: "0", count: 64)
    )
    preBriefing.childSessionId = "sibling"
    preBriefing.transcriptPath = sibling.path
    check("a pre-briefing identity pair survives an ownership polling beat",
          !Orchestrator.noteTranscriptProof(in: &preBriefing)
            && preBriefing.childSessionId == "sibling"
            && preBriefing.transcriptPath == sibling.path)
    let ready = "❯\n\n  ? for shortcuts"
    expect("the surviving path keeps the second briefing attempt reachable",
           Orchestrator.briefingDecision(screen: ready, assistant: .claude,
                                         transcript: nil,
                                         transcriptKnown: preBriefing.transcriptPath != nil,
                                         taskID: ownershipTaskID, attempts: 1,
                                         secondsSinceAttempt: 60), .send)
    expect("the surviving path keeps the briefing ceiling reachable",
           Orchestrator.briefingDecision(screen: ready, assistant: .claude,
                                         transcript: nil,
                                         transcriptKnown: preBriefing.transcriptPath != nil,
                                         taskID: ownershipTaskID,
                                         attempts: Orchestrator.briefingAttemptLimit,
                                         secondsSinceAttempt: 60), .exhausted)

    let processStart = Date(timeIntervalSince1970: 2_000)
    var malformedBeforeRestart = preBriefing
    malformedBeforeRestart.childPID = 100
    malformedBeforeRestart.childProcStart = processStart
    malformedBeforeRestart.childSessionId = "restored-session"
    malformedBeforeRestart.transcriptProven = true
    var restarted = Orchestrator.task(from: Orchestrator.stored(malformedBeforeRestart))!
    let matchingProcessWithoutRegistry = Orchestrator.ChildObservation(
        pid: 100, procStart: processStart
    )
    let restoredProofWasDropped = !restarted.transcriptProven
    let identityStayedProvisional = Orchestrator.identityStep(
        for: restarted, seeing: matchingProcessWithoutRegistry
    ) == .none
    let siblingWasDisproved = Orchestrator.transcriptOwnership(
        sibling, assistant: .claude, taskID: ownershipTaskID
    ) == .other
    let ownershipChanged = Orchestrator.noteTranscriptProof(in: &restarted)
    let finalDecision = Orchestrator.briefingDecision(
        screen: ready, assistant: .claude, transcript: nil,
        transcriptKnown: restarted.transcriptPath != nil, taskID: ownershipTaskID,
        attempts: Orchestrator.briefingAttemptLimit, secondsSinceAttempt: 60
    )
    // Removing the pre-briefing ownership guard must clear the malformed restored pair instead
    // of preserving this fail-closed path through the bounded retry ceiling.
    check("a restarted malformed pair fails closed at the briefing ceiling",
          restoredProofWasDropped
            && restarted.childPID == 100 && restarted.childProcStart == processStart
            && restarted.childSessionId == "restored-session"
            && restarted.transcriptPath == sibling.path
            && identityStayedProvisional && siblingWasDisproved && !ownershipChanged
            && finalDecision == .exhausted)

    var disproved = task(assistant: .claude, childSession: "sibling", transcript: sibling)
    check("a readable transcript belonging elsewhere changes the restored identity",
          Orchestrator.applyTranscriptOwnership(.other, transcript: sibling, to: &disproved))
    check("and clears both halves so discovery can locate this task again",
          disproved.childSessionId == nil && disproved.transcriptPath == nil)

    let missing = dir.appendingPathComponent("missing.jsonl")
    var unavailable = task(assistant: .claude, childSession: "still-possible", transcript: missing)
    check("an unavailable transcript is not treated as a disproof",
          !Orchestrator.applyTranscriptOwnership(.unavailable, transcript: missing,
                                                  to: &unavailable))
    check("and keeps the pair for a later retry",
          unavailable.childSessionId == "still-possible"
            && unavailable.transcriptPath == missing.path)

    let codex = dir.appendingPathComponent("codex.jsonl")
    let codexReceipt = """
    {"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage",\
    "content":[{"type":"text","text":"You are a Clawdline CHILD agent for task 12345678-1234-1234-1234-123456789abc."}]}}}
    """
    try! Data((codexReceipt + "\n").utf8).write(to: codex)
    var unpaired = Orchestrator.Task(
        id: "12345678-1234-1234-1234-123456789abc", state: .briefed, kind: "custom",
        title: "a task", assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
        created: Date(), secretHash: String(repeating: "0", count: 64)
    )
    unpaired.transcriptPath = codex.path
    check("a Codex marker without a rollout id cannot prove an identity pair",
          !Orchestrator.applyTranscriptOwnership(.belongs, transcript: codex, to: &unpaired)
            && !unpaired.transcriptProven)

    let threadID = "01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8"
    let pairedCodex = dir.appendingPathComponent("paired-codex.jsonl")
    let head = """
    {"type":"session_meta","payload":{"session_id":"01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8","cwd":"/tmp",\
    "originator":"codex-tui"}}
    """
    try! Data((head + "\n" + codexReceipt + "\n").utf8).write(to: pairedCodex)
    unpaired.transcriptPath = pairedCodex.path
    check("the same marker becomes proof once Codex establishes the paired id",
          Orchestrator.applyTranscriptOwnership(.belongs, transcript: pairedCodex, to: &unpaired)
            && unpaired.transcriptProven && unpaired.childSessionId == threadID)

    let startingCodex = dir.appendingPathComponent("starting-codex.jsonl")
    try! Data((head + "\n").utf8).write(to: startingCodex)
    expect("an unbriefed Codex rollout is not evidence of another task",
           Orchestrator.transcriptOwnership(startingCodex, assistant: .codex,
                                            taskID: unpaired.id), .unavailable)
}

group("transcript rendering") {
    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let text = Transcript.render(Transcript.parse(sampleTranscript), size: 11.5, mono: mono).string
    check("the speaker is named", text.contains("YOU"))
    check("so is the other one", text.contains("CLAUDE"))
    check("the prose is there", text.contains("add a retry"))
    check("tool calls are marked", text.contains("⏺"))
    check("the tool is named", text.contains("Bash"))
    check("results are marked", text.contains("→"))

    // A fenced block should lose its fence and keep its contents.
    let fenced = [Transcript.Entry(kind: .assistant, text: "run:\n```bash\nls -l\n```\ndone",
                                  tool: nil, time: nil)]
    let out = Transcript.render(fenced, size: 11.5, mono: mono)
    check("fences are removed", !out.string.contains("```"))
    check("the code survives", out.string.contains("ls -l"))
    check("the language tag is not printed as content", !out.string.contains("bash\n"))

    // The code should actually be set in the mono face, which is the point of the exercise.
    if let range = out.string.range(of: "ls -l") {
        let i = out.string.distance(from: out.string.startIndex, to: range.lowerBound)
        let f = out.attribute(.font, at: i, effectiveRange: nil) as? NSFont
        expect("code is monospace", f?.fontName, mono.fontName)
    } else {
        check("found the code run", false)
    }
}
}
