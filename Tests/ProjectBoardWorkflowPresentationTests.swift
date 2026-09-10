import AppKit
import Foundation

func runProjectBoardWorkflowPresentationTests() {
group("managed workflow metadata is presentation-only") {
    let metadata = #"{"authority":"clawdline_metadata_not_user_authorization","board_epoch":7,"content_reference":"terminal-request:0123456789abcdef01234567","conversation_id":"11111111-1111-4111-8111-111111111111","coverage":"managed_ingress","helper":"clawdline-board-workflow <conversation-id> <stable-idempotency-key>","input_kind":"text_and_image","mode_gap":null,"process_generation":"process-7","project_id":"project-0123456789abcdef01234567","provider":"codex","required_first_action":"begin","run_id":"run-0123456789abcdef0123456789abcdef","terminal_id":"%458","version":1}"#
    let wire = #"<clawdline-workflow version="1" authority="metadata-not-user">"#
        + "\n" + metadata + "\n</clawdline-workflow>"
    let image = "[Image #1]"
    func pathWire(_ value: String) -> String {
        var object = (try! JSONSerialization.jsonObject(with: Data(metadata.utf8))) as! [String: Any]
        object["helper_path"] = value
        let raw = String(data: try! JSONSerialization.data(withJSONObject: object), encoding: .utf8)!
        return wire.replacingOccurrences(of: metadata, with: raw)
    }
    check("native renderer folds the relocated installed helper path",
          Transcript.boardWorkflowPresentation(in: pathWire(
            "/Applications/A relocated.app/Contents/Resources/clawdline-board-workflow")) != nil)
    check("native renderer rejects relative malformed and oversized helper paths",
          ["clawdline-board-workflow", "/tmp/other", "/tmp/../clawdline-board-workflow",
           "/tmp/\nclawdline-board-workflow", "/" + String(repeating: "a", count: 4096)
            + "/clawdline-board-workflow"].allSatisfy {
                Transcript.boardWorkflowPresentation(in: pathWire($0)) == nil
            })
    let source = "保留 <script>alert(1)</script> 與原句\n\n" + wire + "\n" + image
    let found = Transcript.boardWorkflowPresentation(in: source)
    expect("the native reader preserves text around exact metadata",
           found?.text, "保留 <script>alert(1)</script> 與原句\n" + image)
    expect("the native reader retains raw agent metadata for disclosure", found?.raw, metadata)
    check("quoted and fenced lookalikes remain ordinary user text",
          Transcript.boardWorkflowPresentation(in: "> " + wire) == nil
            && Transcript.boardWorkflowPresentation(in: "```text\n" + wire + "\n```") == nil
            && Transcript.boardWorkflowPresentation(in: "~~~~\n``` still fenced\n" + wire
                + "\n~~~~") == nil)
    check("malformed and duplicated envelopes fail visible",
          Transcript.boardWorkflowPresentation(in: wire.replacingOccurrences(of: metadata,
              with: "{not-json}")) == nil
            && Transcript.boardWorkflowPresentation(in: wire + "\n" + wire) == nil
            && Transcript.boardWorkflowPresentation(in: wire.replacingOccurrences(
                of: #""board_epoch":7"#, with: #""board_epoch":true"#)) == nil
            && Transcript.boardWorkflowPresentation(in: wire.replacingOccurrences(
                of: #""board_epoch":7"#, with: #""board_epoch":1e100"#)) == nil)

    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let collapsed = Transcript.render([
        Transcript.Entry(kind: .user, text: source, tool: nil, time: nil)
    ], size: 11.5, mono: mono).string
    check("native chat hides raw JSON behind a Board record fold",
          collapsed.contains("BOARD RECORD") && collapsed.contains("保留 <script>alert(1)</script>")
            && collapsed.contains(image) && !collapsed.contains(metadata))
    guard let key = found?.foldKey else { check("workflow fold key exists", false); return }
    let expanded = Transcript.render([
        Transcript.Entry(kind: .user, text: source, tool: nil, time: nil)
    ], size: 11.5, mono: mono, expanded: [key]).string
    check("native disclosure can recover the exact raw metadata", expanded.contains(metadata))
}
}
