import Foundation

// What a child is told when it is dispatched. Almost all of this file is the briefing text
// itself, which is data rather than logic, and it was the largest block in Orchestrator.swift
// that reads nothing the lock protects. `policySection()` comes with it: it has exactly one
// caller, and Swift's `private` would have made it invisible from here.
extension Orchestrator {
    /// What this Mac has said about itself, for every child briefing.
    ///
    /// **This travelled with the dispatch recipe once, and that was the wrong home for it.**
    /// The file was reasoned about as rules for *handing work out*, so when the tree lost its
    /// second level the section read as dead weight and was deleted with the recipe. It is not
    /// dead weight: the same file is where a person writes down what is true of this machine,
    /// and one of those sentences can name a network constraint that changes how a child uses its
    /// HTTP fast path. A leaf reads it and behaves differently, which is the whole test of whether
    /// a paragraph belongs in a briefing. So it goes to every child, dispatcher or not, and there
    /// is no longer any such thing as the second kind.
    ///
    /// Read from disk at briefing time, so an edit reaches the next child rather than the next
    /// launch. Empty when nobody has written anything, rather than a heading with nothing under
    /// it.
    private static func policySection() -> String {
        guard let policy = policy() else { return "" }
        return """


        ## What this Mac says

        House rules and machine facts, from \(policyURL.path) and its optional local sibling at
        \(localPolicyURL.path). They are the person's, not this app's; where they and your own
        judgement disagree, follow them and say so in your summary.

        \(policy)
        """
    }


    /// The ownership marker rides in the typed line and not only in CHILD.md, because an assistant
    /// answers the line before it opens the file — and the first thing on the screen should be
    /// what the Session was sent to do, in the language of whoever is looking.
    static func firstLine(id: String, secret: String, announce: String? = nil,
                          sessionRoot: Bool = false) -> String {
        let opening = announce.map { "Say this line first, verbatim: \($0) Then read" } ?? "Read"
        let ownership = sessionRoot ? "ROOT" : "CHILD"
        return "You are a Clawdline \(ownership) agent for task \(id). "
            + "\(opening) /tmp/.clawdline/\(id)/CHILD.md and follow it exactly. TASK_SECRET=\(secret)"
    }

    /// The interface language, named twice — in English for the assistant reading the briefing,
    /// and in itself for the person reading over its shoulder: "Traditional Chinese (繁體中文)".
    static var languageName: String { rootAssignmentLanguage().name }

    /// The dependency-free validator every child runs before publishing its result. It is
    /// encoded only so the generated shell command keeps the program in one argument. The
    /// focused Node test decodes this value and requires it to match the repository-local tool.
    /// Shipping the program in the briefing, rather than looking under `task.projectDir`, lets
    /// a child working in any unrelated repository perform the same preflight.
    static let childResultValidatorProgramBase64 = "Y29uc3QgeyBjaG1vZFN5bmMsIHJlYWRGaWxlU3luYywgcmVuYW1lU3luYywgd3JpdGVGaWxlU3luYyB9ID0gYXdhaXQgaW1wb3J0KCJub2RlOmZzIik7CmNvbnN0IHsgY3JlYXRlSGFzaCB9ID0gYXdhaXQgaW1wb3J0KCJub2RlOmNyeXB0byIpOwoKY29uc3QgaW52YWxpZCA9IChyZWFzb24pID0+IHsKICAgIGNvbnNvbGUuZXJyb3IoYHRhc2sgcmVzdWx0IHByZWZsaWdodDogaW52YWxpZCDigJQgJHtyZWFzb259YCk7CiAgICBwcm9jZXNzLmV4aXQoMSk7Cn07CmNvbnN0IG9iamVjdCA9ICh2YWx1ZSkgPT4gdmFsdWUgIT09IG51bGwgJiYgdHlwZW9mIHZhbHVlID09PSAib2JqZWN0IiAmJiAhQXJyYXkuaXNBcnJheSh2YWx1ZSk7CmNvbnN0IGV4YWN0S2V5cyA9ICh2YWx1ZSwga2V5cykgPT4gb2JqZWN0KHZhbHVlKQogICAgJiYgT2JqZWN0LmtleXModmFsdWUpLmxlbmd0aCA9PT0ga2V5cy5sZW5ndGgKICAgICYmIGtleXMuZXZlcnkoKGtleSkgPT4gT2JqZWN0Lmhhc093bih2YWx1ZSwga2V5KSk7CmNvbnN0IG5vbkVtcHR5ID0gKHZhbHVlLCBtYXhpbXVtKSA9PiB0eXBlb2YgdmFsdWUgPT09ICJzdHJpbmciCiAgICAmJiB2YWx1ZS50cmltKCkubGVuZ3RoID4gMCAmJiB2YWx1ZS5sZW5ndGggPD0gbWF4aW11bTsKY29uc3QgdGFza0lEID0gKHZhbHVlKSA9PiB0eXBlb2YgdmFsdWUgPT09ICJzdHJpbmciICYmIHZhbHVlLmxlbmd0aCA9PT0gMzYKICAgICYmIC9eW2EtZjAtOS1dKyQvLnRlc3QodmFsdWUpOwpjb25zdCB0YXNrU2VjcmV0ID0gKHZhbHVlKSA9PiB0eXBlb2YgdmFsdWUgPT09ICJzdHJpbmciICYmIC9eW2EtZjAtOV17NjR9JC8udGVzdCh2YWx1ZSk7CmNvbnN0IHNsdWcgPSAodmFsdWUpID0+IHR5cGVvZiB2YWx1ZSA9PT0gInN0cmluZyIgJiYgdmFsdWUubGVuZ3RoID4gMCAmJiB2YWx1ZS5sZW5ndGggPD0gNjQKICAgICYmICF2YWx1ZS5zdGFydHNXaXRoKCItIikgJiYgL15bYS16MC05Ll8tXSskLy50ZXN0KHZhbHVlLnRvTG93ZXJDYXNlKCkpOwoKY29uc3QgcmVhZEpTT04gPSAocGF0aCwgbGFiZWwpID0+IHsKICAgIHRyeSB7CiAgICAgICAgY29uc3QgYnl0ZXMgPSByZWFkRmlsZVN5bmMocGF0aCk7CiAgICAgICAgY29uc3QgdmFsdWUgPSBKU09OLnBhcnNlKGJ5dGVzLnRvU3RyaW5nKCJ1dGY4IikpOwogICAgICAgIGlmICghb2JqZWN0KHZhbHVlKSkgaW52YWxpZChgJHtsYWJlbH0gbXVzdCBjb250YWluIG9uZSBKU09OIG9iamVjdGApOwogICAgICAgIHJldHVybiB7IHZhbHVlLCBieXRlcyB9OwogICAgfSBjYXRjaCB7CiAgICAgICAgaW52YWxpZChgJHtsYWJlbH0gaXMgbm90IHJlYWRhYmxlIEpTT05gKTsKICAgIH0KfTsKCmNvbnN0IGFyZ3MgPSBwcm9jZXNzLmFyZ3Yuc2xpY2UoMik7CmNvbnN0IGhhc1JlYWR5UGF0aCA9IGFyZ3MubGVuZ3RoID49IDM7CmNvbnN0IFt0YXNrUGF0aCwgcmVzdWx0UGF0aCwgcmVhZHlQYXRoXSA9IGhhc1JlYWR5UGF0aCA/IGFyZ3Muc2xpY2UoLTMpIDogWy4uLmFyZ3Muc2xpY2UoLTIpLCB1bmRlZmluZWRdOwppZiAoIXRhc2tQYXRoIHx8ICFyZXN1bHRQYXRoIHx8IHRhc2tQYXRoID09PSByZXN1bHRQYXRoCiAgICB8fCAocmVhZHlQYXRoICYmIChyZWFkeVBhdGggPT09IHRhc2tQYXRoIHx8IHJlYWR5UGF0aCA9PT0gcmVzdWx0UGF0aCkpKSB7CiAgICBpbnZhbGlkKCJ1c2FnZTogdmFsaWRhdG9yIHRhc2suanNvbiByZXN1bHQuanNvbi50bXAgW3Jlc3VsdC5qc29uLnJlYWR5XSIpOwp9CmNvbnN0IHsgdmFsdWU6IHRhc2sgfSA9IHJlYWRKU09OKHRhc2tQYXRoLCAidGFzay5qc29uIik7CmNvbnN0IHsgdmFsdWU6IHJlc3VsdCwgYnl0ZXM6IHJlc3VsdEJ5dGVzIH0gPSByZWFkSlNPTihyZXN1bHRQYXRoLCAicmVzdWx0Lmpzb24udG1wIik7CgppZiAodGFzay5jbGF3ZGxpbmVfcHJvdG9jb2wgIT09IDEgfHwgIXRhc2tJRCh0YXNrLnRhc2tfaWQpKSB7CiAgICBpbnZhbGlkKCJ0YXNrLmpzb24gaGFzIG5vIHZhbGlkIHByb3RvY29sIGlkZW50aXR5Iik7Cn0KaWYgKHJlc3VsdC5jbGF3ZGxpbmVfcHJvdG9jb2wgIT09IDEpIGludmFsaWQoImNsYXdkbGluZV9wcm90b2NvbCBtdXN0IGJlIDEiKTsKaWYgKCF0YXNrSUQocmVzdWx0LnRhc2tfaWQpIHx8IHJlc3VsdC50YXNrX2lkICE9PSB0YXNrLnRhc2tfaWQpIHsKICAgIGludmFsaWQoInRhc2tfaWQgbXVzdCBiZSBhIGxvd2VyY2FzZSBVVUlEIG1hdGNoaW5nIHRhc2suanNvbiIpOwp9CmlmICghdGFza1NlY3JldChyZXN1bHQudGFza19zZWNyZXQpKSBpbnZhbGlkKCJ0YXNrX3NlY3JldCBtdXN0IGJlIDY0IGxvd2VyY2FzZSBoZXhhZGVjaW1hbCBjaGFyYWN0ZXJzIik7CmlmIChyZXN1bHQuc3RhdHVzICE9PSAic3VjY2VzcyIgJiYgcmVzdWx0LnN0YXR1cyAhPT0gImZhaWx1cmUiKSB7CiAgICBpbnZhbGlkKCJzdGF0dXMgbXVzdCBiZSBzdWNjZXNzIG9yIGZhaWx1cmUiKTsKfQoKaWYgKE9iamVjdC5oYXNPd24ocmVzdWx0LCAidmVyaWZpY2F0aW9uIikpIHsKICAgIGNvbnN0IHJvdyA9IHJlc3VsdC52ZXJpZmljYXRpb247CiAgICBpZiAoIW9iamVjdChyb3cpCiAgICAgICAgfHwgIU51bWJlci5pc0ludGVnZXIocm93LnJ1bnMpIHx8IHJvdy5ydW5zIDwgMAogICAgICAgIHx8ICFOdW1iZXIuaXNJbnRlZ2VyKHJvdy5zZWNvbmRzKSB8fCByb3cuc2Vjb25kcyA8IDAKICAgICAgICB8fCAhWyJwYXNzIiwgImZhaWwiLCAic2tpcHBlZCJdLmluY2x1ZGVzKHJvdy5sYXN0KQogICAgICAgIHx8ICFub25FbXB0eShyb3cuc2NvcGUsIDMwMCkpIHsKICAgICAgICBpbnZhbGlkKCJ2ZXJpZmljYXRpb24gbXVzdCBjb250YWluIG5vbi1uZWdhdGl2ZSBpbnRlZ2VyIHJ1bnMvc2Vjb25kcywgYSB2YWxpZCBsYXN0IHZhbHVlLCBhbmQgYSBub24tZW1wdHkgc2NvcGUiKTsKICAgIH0KfQoKY29uc3QgZ3JhcGhOb2RlID0gb2JqZWN0KHRhc2suZ3JhcGgpICYmIEFycmF5LmlzQXJyYXkodGFzay5ncmFwaC5ub2RlcykKICAgID8gdGFzay5ncmFwaC5ub2Rlcy5maW5kKChub2RlKSA9PiBvYmplY3Qobm9kZSkgJiYgbm9kZS5pZCA9PT0gdGFzay5ncmFwaC5jdXJyZW50X25vZGUpCiAgICA6IHVuZGVmaW5lZDsKY29uc3Qga2luZFdvcmRzID0gdHlwZW9mIHRhc2sua2luZCA9PT0gInN0cmluZyIKICAgID8gdGFzay5raW5kLnRvTG93ZXJDYXNlKCkuc3BsaXQoL1teXHB7TH1ccHtOfV0rL3UpLmZpbHRlcihCb29sZWFuKSA6IFtdOwpjb25zdCByZXF1aXJlc1JldmlldyA9IGdyYXBoTm9kZSA/IGdyYXBoTm9kZS5raW5kID09PSAicmV2aWV3IiA6IGtpbmRXb3Jkcy5pbmNsdWRlcygicmV2aWV3Iik7Cgpjb25zdCB2YWxpZGF0ZVJldmlldyA9IChyZXZpZXcpID0+IHsKICAgIGlmICghZXhhY3RLZXlzKHJldmlldywgWyJ2ZXJkaWN0IiwgImF4ZXMiXSkKICAgICAgICB8fCAhWyJzYWZlX3RvX2xhbmQiLCAiY2hhbmdlc19yZXF1aXJlZCJdLmluY2x1ZGVzKHJldmlldy52ZXJkaWN0KQogICAgICAgIHx8ICFBcnJheS5pc0FycmF5KHJldmlldy5heGVzKSB8fCByZXZpZXcuYXhlcy5sZW5ndGggIT09IDMpIHsKICAgICAgICBpbnZhbGlkKCJyZXZpZXcgbXVzdCBjb250YWluIG9ubHkgYSB2YWxpZCB2ZXJkaWN0IGFuZCBleGFjdGx5IHRocmVlIGF4ZXMiKTsKICAgIH0KICAgIGNvbnN0IHdhbnRlZEF4ZXMgPSBuZXcgU2V0KFsic3BlY2lmaWNhdGlvbiIsICJyZXBvc2l0b3J5X2ludmFyaWFudHMiLCAicnVudGltZV9mYWlsdXJlX2JlaGF2aW9yIl0pOwogICAgY29uc3Qgc2VlbkF4ZXMgPSBuZXcgU2V0KCk7CiAgICBsZXQgZmluZGluZ0NvdW50ID0gMDsKICAgIGZvciAoY29uc3QgYXhpcyBvZiByZXZpZXcuYXhlcykgewogICAgICAgIGlmICghZXhhY3RLZXlzKGF4aXMsIFsiYXhpcyIsICJzdGF0dXMiLCAiZmluZGluZ3MiXSkKICAgICAgICAgICAgfHwgIXdhbnRlZEF4ZXMuaGFzKGF4aXMuYXhpcykgfHwgc2VlbkF4ZXMuaGFzKGF4aXMuYXhpcykKICAgICAgICAgICAgfHwgIVsicGFzcyIsICJmaW5kaW5ncyJdLmluY2x1ZGVzKGF4aXMuc3RhdHVzKQogICAgICAgICAgICB8fCAhQXJyYXkuaXNBcnJheShheGlzLmZpbmRpbmdzKSB8fCBheGlzLmZpbmRpbmdzLmxlbmd0aCA+IDMyKSB7CiAgICAgICAgICAgIGludmFsaWQoInJldmlldyBheGVzIG11c3QgYmUgdW5pcXVlLCBjbG9zZWQsIG5hbWVkIGF4ZXMgd2l0aCB2YWxpZCBzdGF0dXMgYW5kIGZpbmRpbmdzIik7CiAgICAgICAgfQogICAgICAgIHNlZW5BeGVzLmFkZChheGlzLmF4aXMpOwogICAgICAgIGNvbnN0IGZpbmRpbmdJRHMgPSBuZXcgU2V0KCk7CiAgICAgICAgZm9yIChjb25zdCBmaW5kaW5nIG9mIGF4aXMuZmluZGluZ3MpIHsKICAgICAgICAgICAgaWYgKCFleGFjdEtleXMoZmluZGluZywgWyJpZCIsICJzZXZlcml0eSIsICJzdW1tYXJ5IiwgImV2aWRlbmNlIl0pCiAgICAgICAgICAgICAgICB8fCAhc2x1ZyhmaW5kaW5nLmlkKSB8fCBmaW5kaW5nSURzLmhhcyhmaW5kaW5nLmlkKQogICAgICAgICAgICAgICAgfHwgIVsiYmxvY2tpbmciLCAiaW1wb3J0YW50IiwgIm1pbm9yIl0uaW5jbHVkZXMoZmluZGluZy5zZXZlcml0eSkKICAgICAgICAgICAgICAgIHx8ICFub25FbXB0eShmaW5kaW5nLnN1bW1hcnksIDUwMCkKICAgICAgICAgICAgICAgIHx8ICFBcnJheS5pc0FycmF5KGZpbmRpbmcuZXZpZGVuY2UpIHx8IGZpbmRpbmcuZXZpZGVuY2UubGVuZ3RoIDwgMQogICAgICAgICAgICAgICAgfHwgZmluZGluZy5ldmlkZW5jZS5sZW5ndGggPiA4CiAgICAgICAgICAgICAgICB8fCBmaW5kaW5nLmV2aWRlbmNlLnNvbWUoKGl0ZW0pID0+ICFub25FbXB0eShpdGVtLCA1MDApKSkgewogICAgICAgICAgICAgICAgaW52YWxpZCgiZWFjaCByZXZpZXcgZmluZGluZyBtdXN0IHVzZSB0aGUgZXhhY3QgaWQvc2V2ZXJpdHkvc3VtbWFyeS9ldmlkZW5jZSBzY2hlbWEiKTsKICAgICAgICAgICAgfQogICAgICAgICAgICBmaW5kaW5nSURzLmFkZChmaW5kaW5nLmlkKTsKICAgICAgICAgICAgZmluZGluZ0NvdW50ICs9IDE7CiAgICAgICAgfQogICAgICAgIGlmICgoYXhpcy5zdGF0dXMgPT09ICJwYXNzIikgIT09IChheGlzLmZpbmRpbmdzLmxlbmd0aCA9PT0gMCkpIHsKICAgICAgICAgICAgaW52YWxpZCgiYSBwYXNzaW5nIGF4aXMgaGFzIG5vIGZpbmRpbmdzIGFuZCBhIGZpbmRpbmdzIGF4aXMgaGFzIGF0IGxlYXN0IG9uZSIpOwogICAgICAgIH0KICAgIH0KICAgIGlmIChzZWVuQXhlcy5zaXplICE9PSB3YW50ZWRBeGVzLnNpemUpIGludmFsaWQoInJldmlldyBtdXN0IGNvbnRhaW4gZWFjaCByZXF1aXJlZCBheGlzIG9uY2UiKTsKICAgIGlmICgocmV2aWV3LnZlcmRpY3QgPT09ICJzYWZlX3RvX2xhbmQiKSAhPT0gKGZpbmRpbmdDb3VudCA9PT0gMCkpIHsKICAgICAgICBpbnZhbGlkKCJyZXZpZXcgdmVyZGljdCBtdXN0IGFncmVlIHdpdGggaXRzIGZpbmRpbmdzIik7CiAgICB9Cn07CgppZiAocmVxdWlyZXNSZXZpZXcgJiYgcmVzdWx0LnN0YXR1cyA9PT0gInN1Y2Nlc3MiICYmICFPYmplY3QuaGFzT3duKHJlc3VsdCwgInJldmlldyIpKSB7CiAgICBpbnZhbGlkKCJhIHN1Y2Nlc3NmdWwgcmV2aWV3IHRhc2sgcmVxdWlyZXMgYSBjbG9zZWQgcmV2aWV3IHJlY2VpcHQiKTsKfQppZiAoT2JqZWN0Lmhhc093bihyZXN1bHQsICJyZXZpZXciKSkgdmFsaWRhdGVSZXZpZXcocmVzdWx0LnJldmlldyk7CgovLyBBIGNoaWxkIG5vcm1hbGx5IHJlbmFtZXMgdGhlIHJlc3VsdCBpbW1lZGlhdGVseS4gSWYgaXRzIHNoZWxsIHN0YWxscyBhZnRlciB2YWxpZGF0aW9uLCB0aGlzCi8vIHRhc2stb3duZWQgbWFya2VyIGxldHMgdGhlIGJyb2tlciByZWNvdmVyIGxhdGVyIHdpdGhvdXQgdHJlYXRpbmcgYWdlIGFzIGNvbnNlbnQuIEl0IGJpbmRzIHRoZQovLyBleGFjdCBieXRlcyB0aGF0IHBhc3NlZCB2YWxpZGF0aW9uOyB0aGUgYnJva2VyIGFkZGl0aW9uYWxseSBjaGVja3MgdGhlIHN0b3JlZCB0YXNrLXNlY3JldCBoYXNoCi8vIGFuZCByZXF1aXJlcyBhbiB1bmNoYW5nZWQgb2JzZXJ2YXRpb24gd2luZG93IGJlZm9yZSBwdWJsaXNoaW5nIHRoZSBmaW5hbCByZXN1bHQuCmlmIChyZWFkeVBhdGgpIHsKICAgIGNvbnN0IG1hcmtlciA9IHsKICAgICAgICBjbGF3ZGxpbmVfcHJvdG9jb2w6IDEsCiAgICAgICAgdGFza19pZDogdGFzay50YXNrX2lkLAogICAgICAgIGZpbmFsaXphdGlvbl9yZWFkeTogdHJ1ZSwKICAgICAgICByZXN1bHRfc2hhMjU2OiBjcmVhdGVIYXNoKCJzaGEyNTYiKS51cGRhdGUocmVzdWx0Qnl0ZXMpLmRpZ2VzdCgiaGV4IiksCiAgICB9OwogICAgY29uc3QgdGVtcG9yYXJ5ID0gYCR7cmVhZHlQYXRofS53cml0aW5nLSR7cHJvY2Vzcy5waWR9YDsKICAgIHdyaXRlRmlsZVN5bmModGVtcG9yYXJ5LCBgJHtKU09OLnN0cmluZ2lmeShtYXJrZXIpfVxuYCwgeyBmbGFnOiAid3giLCBtb2RlOiAwbzYwMCB9KTsKICAgIGNobW9kU3luYyh0ZW1wb3JhcnksIDBvNjAwKTsKICAgIHJlbmFtZVN5bmModGVtcG9yYXJ5LCByZWFkeVBhdGgpOwp9Cgpjb25zb2xlLmxvZygidGFzayByZXN1bHQgcHJlZmxpZ2h0OiB2YWxpZCIpOwo="

    static func rootAssignmentLanguage(copy: Copy = L.t) -> RootAssignmentLanguage {
        let tag = L.tag(of: copy)
        let english: String
        switch tag {
        case "zh-Hant": english = "Traditional Chinese"; case "zh-Hans": english = "Simplified Chinese"
        default: english = Locale(identifier: "en").localizedString(forIdentifier: tag) ?? tag
        }
        let native = Locale(identifier: tag).localizedString(forIdentifier: tag) ?? tag
        return RootAssignmentLanguage(tag: tag,
            name: english == native ? english : "\(english) (\(native))")
    }

    /// How many levels of dispatch this Mac has: one. A root opens children, and a child is the
    /// bottom — it opens nothing.
    ///
    /// **A constant rather than a setting, and that is the whole point.** This used to be read
    /// out of `orchestrator_max_grandchildren`, which meant the depth of the tree was a number
    /// in a file. Two things are wrong with that. `config.json` is seeded once and never
    /// migrated, so changing the default would have left every Mac that had already run this app
    /// dispatching grandchildren for ever; and a rule that a hand-edit can undo is not a rule,
    /// it is a preference. What a child needs when a job is too big for one session is its own
    /// assistant's subagents — Claude Code's Task tool, Codex's subagents — which cost no
    /// terminal tab, no broker capacity and no second level of supervision.
    static let depthFloor = 1

    /// Whether a task at this depth may exist at all. `depth` is the new task's own level: 1 for
    /// a root's child, 2 for anything a child tries to open. Pure, so the one-level rule can be
    /// checked without a broker, a terminal or a config file.
    static func depthIsAllowed(_ depth: Int) -> Bool { depth <= depthFloor }

    /// Where a task's heavy scratch goes and what becomes of it, said differently to the two
    /// sessions that read a briefing, because the sentence a child can rely on — everything in
    /// `work/` goes when the task ends — is untrue about everything a Root Session writes after its
    /// first task. Both point at the scratch tool, whose contract is `docs/scratch.md`.
    static func scratchRule(for task: Task, dir: String) -> String {
        task.sessionRoot
            ? """
              - Heavyweight temporary work for this first task (repo copies, build outputs, mutation
                worktrees and compiler indexes) goes in \(dir)/work/, not in the assistant scratchpad,
                and it is deleted when that task ends — immediately on success, after the configured
                grace period otherwise.
                **This Session outlives that task, and its later work does not go in \(dir)/work/.**
                A `work/` that appears there again is deleted once this Session's process has exited,
                and nothing in it is kept. Scratch for later turns belongs in the owned scratch root,
                `${CLAWDLINE_SCRATCH_ROOT:-/tmp/clawdline-scratch}`, made with
                `tools/scratch.sh new <purpose>` (contract: `docs/scratch.md`). Its marker names this
                Session as the owner, so the broker removes it only after this Session has exited;
                remove what you made with `tools/scratch.sh remove <path>` when you are done, and a
                credential copy before the turn that made it ends.
              """
            : """
              - Put heavyweight temporary work (repo copies, build outputs, mutation worktrees and
                compiler indexes) in \(dir)/work/, not in the assistant scratchpad. Make it with the
                scratch tool rooted there — `tools/scratch.sh new <purpose> --root \(dir)/work`, or
                `tools/scratch.sh snapshot-run --subject worktree --root \(dir)/work -- <command>`
                (contract: `docs/scratch.md`) — or directly in that directory where the tool is not
                available. Everything there is deleted when the task ends — immediately on success,
                after the configured grace period otherwise — so copy any log or diff worth keeping
                into `artifacts/` **before** writing `result.json`.
              """
    }

    /// The verification `TMPDIR` half of the same rule.
    static func verificationScratchRule(for task: Task, dir: String) -> String {
        task.sessionRoot
            ? "Point this first task's verification `TMPDIR` at `\(dir)/work/tmp`, which goes with "
                + "the task; later verification in this Session runs through "
                + "`tools/scratch.sh snapshot-run`, whose entry and test binary go when the run ends."
            : "Run verification through `tools/scratch.sh snapshot-run --subject worktree --root "
                + "\(dir)/work -- ./test.sh`, which points `TMPDIR` at `\(dir)/work/<entry>/tmp`, or "
                + "point `TMPDIR` at `\(dir)/work/tmp` where the tool is not available; either way "
                + "the test binary is reclaimed with the task."
    }

    static func childBrief(for task: Task) -> String {
        let dir = "/tmp/.clawdline/\(task.id)"
        let workspaceRule: String
        let isolationSection: String
        if let worktree = task.worktree {
            workspaceRule = "- Work inside \(worktree.cwd). Commit repository changes there; put "
                + "non-repository artifacts in \(dir)/artifacts/."
            isolationSection = """

            ## Your isolated checkout

            This is a fresh checkout of commit `\(worktree.base)` on branch `\(worktree.branch)`.
            Uncommitted files from the base repository are deliberately absent. Files ignored by
            gitignore — dependencies, build caches, and local environment files — are absent too;
            install them only after checking that doing so will not consume most of your timeout.

            \(task.assistant == .codex
              ? """
                **Do not commit. Leave your work uncommitted in this checkout.** A linked
                worktree keeps its git metadata in the base repository's
                `.git/worktrees/<task-id>/`, which is outside what you can write, so `git add`
                fails with `Operation not permitted` on `index.lock` and the delivery is
                reported as a failure with the work done. That has cost this repository whole
                rounds. The root reads this checkout and commits for you; your delivery is the
                bytes, not a branch. You may use `git status`, `git diff`, `git log` and
                `git show` to read.
                """
              : """
                **Commit early and often.** Commit only on this branch: the branch is the
                delivery, and uncommitted changes can be lost when the checkout is cleaned. You
                may use `git add`, `git commit`, `git status`, `git diff`, `git log`, and
                `git show` here.
                """)
            Do not push, switch or check out another branch, rebase, merge, hard-reset, stash,
            use `--git-dir` or `git -C` to reach the base repository, run any `git worktree`
            command, or run `./build.sh`. The app records commits, HEAD and dirty state from git;
            these rules are briefing rules rather than a shell sandbox.

            This checkout's `.build/` is reclaimed on the same schedule as `work/` once the task
            ends, and any git-ignored `node_modules` or `.venv` in it on that schedule once your
            process has exited. The source and the delivery branch are never touched by that, but
            nothing you want to keep should be left inside a build or dependency directory. Once
            the root records this delivery as landed, the checkout itself is removed after its
            uncommitted changes are preserved and verified; the branch is kept.
            """
        } else {
            workspaceRule = "- Work inside \(task.projectDir). Put every file you produce in "
                + "\(dir)/artifacts/\n  (create the directory if it is missing)."
            isolationSection = ""
        }
        // Where this one stands, said plainly and once. Written into the briefing rather than
        // left to be discovered, because a child that finds out by being refused has already
        // spent a turn on it — and one that assumes it may dispatch spends several. The second
        // sentence is the part that changes behaviour rather than only forbidding it: the work
        // that used to be handed to a grandchild is work an assistant's own subagents do,
        // without a terminal tab, a briefing or a level of supervision under this one.
        let handOnRule = task.sessionRoot
            ? "**You are a Root Session. You may dispatch Clawdline child tasks of your own.** "
                + "You own their synthesis, integration, verification, and landing."
            : "**You are the bottom of this tree: you cannot dispatch Clawdline tasks "
                + "of your own, and a request to open one is refused.** When part of this needs "
                + "to run in parallel or wants a context of its own, use your own assistant's "
                + "built-in subagents (Claude Code's Task tool, Codex's subagents). They cost no "
                + "terminal tab and no broker capacity, and their answers come back to you rather "
                + "than to a file."
        let briefingHeading = task.sessionRoot
            ? "# Clawdline Root Session briefing — task \(task.id)"
            : "# Clawdline child briefing — task \(task.id)"
        let ownershipIntro = task.sessionRoot
            ? "You are an independently owned Clawdline Root Session. Your first bounded job is "
                + "described in \(dir)/task.json — read that file now. Reporting that task does "
                + "not end this Session; leave it ready for its own next turn."
            : "You are a CHILD session working for a Clawdline root session. Your one job is the "
                + "task described in \(dir)/task.json — read that file now."
        let landingRule = task.sessionRoot
            ? "You are the root for this line of work and retain responsibility for integration, "
                + "verification, and landing after the first task receipt is written."
            : "Landing records belong to the root after delivery; by protocol convention, a "
                + "child does not call its task's `/landing` route itself even though it holds "
                + "that task's secret."
        // What this Mac has said about itself, for every child rather than for a dispatcher.
        // See `policySection()`; it is empty when nobody has written anything.
        let houseRules = policySection()
        let verificationMinutes = task.timeoutMinutes % 3 == 0
            ? String(task.timeoutMinutes / 3)
            : String(format: "%.1f", Double(task.timeoutMinutes) / 3.0)
        let attachedSection = task.attachSessionId == nil ? "" : """

        ## Your standing session

        This task was attached to a standing session instead of opening a new tab. Finishing,
        failing or cancelling this task does not end this session; after `result.json` is written,
        leave the tab ready for the next complete follow-up task.

        Clawdline recorded that this process was launched with access to the whole
        `/tmp/.clawdline` task root; sessions given only their original task directory are
        refused before a follow-up is typed. This follow-up did not open the tab, however: if any
        permission, plan or confirmation menu appears, leave it for the session's owner.
        Clawdline does not choose from a menu on a session this task did not open. If the briefing
        is still unaccepted when this task's timeout expires, the task ends as `timeout` and
        releases the standing session and its claims.

        """
        // Loopback reachability belongs to this particular launched session, not to the assistant
        // name. Both assistants can run with or without it, and Task has no durable capability
        // receipt from inside the child. Offer the HTTP path to both; progress and completion keep
        // their file signals, and every HTTP instruction says what a refusal or failed connection
        // means instead of pretending the assistant name answered the runtime question.
        let progressFile = """
        ```json
        {"task_secret": "<the TASK_SECRET value from your first message>",
         "note": "<one sentence, at most \(progressLimit) characters>"}
        ```
        """
        let timelySection = """
              ## Up to 5 timely notifications, when the user is waiting

              You may use your own TASK_SECRET to push one sentence the user needs to know now,
              before completion or for 60 seconds afterwards:

              ```bash
              curl --fail-with-body -sS -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/notify \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \\
                -H 'Content-Type: application/json' \\
                -d '{"title":"<at most 80 characters>","body":"<at most 500 characters>"}'
              ```

              **`--fail-with-body` is not decoration, and it is on every command in this
              briefing.** Plain `curl` exits 0 whatever the server says: a `401` from a stale
              secret and a `200` are the same exit status, so a request that was refused reads
              exactly like one that arrived. With the flag the server's typed error still
              prints and the command exits non-zero — measured on this Mac, a wrong task secret
              answers `403 forbidden` and exits 22. **Look at that status before you say you
              sent something.**

              The value of push is rarity. Routine results belong in `result.json`; notify only
              when the user is waiting for the answer, including a scheduled task such as
              today's weather whose useful output is the notification itself. Empty title/body
              values are refused. Each task may send at most 5 notifications, and this Mac
              accepts at most 30 per hour. The user may turn agent notifications off. A `409
              agent_notify_disabled` response is not your fault, and neither is any other
              refusal here: leave the content in `result.json`, report failure honestly,
              and do not retry.
              """
        let progressChannel = """
              ```bash
              curl --fail-with-body -sS -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/progress \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \\
                -H 'Content-Type: application/json' \\
                -d '{"note":"<one sentence, at most \(progressLimit) characters>"}'
              ```

              **A non-zero exit means the note was not recorded.** `--fail-with-body` prints
              what the server said and then fails; without it a `401` or a `404` exits 0 and
              you carry on believing the note is on somebody's screen. A refusal here is
              usually the secret: it is the TASK_SECRET from your first message and nothing
              else.

              If that `curl` cannot connect — some sandboxes have no loopback — or it keeps
              being refused, do not keep retrying it: write \(dir)/progress.json with your
              file-writing tool instead, replacing the whole file each time,

              \(progressFile)

              and the broker collects it the way it collects `result.json`.
              """
        let inflightSection = """
              ## Before you start work you believe is new, look

              Another session's isolated checkout is invisible from the shared tree: a finished
              delivery sitting on a branch nobody has merged shows up in no `git status`, no
              `git diff` and no file listing. So "nothing here does that yet" is not evidence.
              This is:

              ```bash
              curl --fail-with-body -sS http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/inflight \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>"
              ```

              Every line of work outstanding in this repository: what it is, who has it, what
              state it is in, what files it claimed, and for isolated ones the branch and head
              where its code actually lives. Read it before you build something you think
              nobody has built. If a row looks like your job, say so in your result rather
              than doing it twice.

              **If this one fails you have no answer, which is not the same as an empty
              board.** Without the flag a refusal prints nothing and exits 0, and nothing on
              the screen is exactly what "no other work" looks like. So on a non-zero exit —
              and on an empty list, which is only what the broker knows about this repository
              at this moment — say in your result what you checked and what it told you,
              rather than reporting the ground as clear.
              """
        let announceSection = """
              Optionally, if outbound network is permitted in your sandbox, you may ALSO announce it:
              `curl --fail-with-body -sS -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/complete \\
                 -H "X-Clawdline-Task-Secret: <TASK_SECRET>" -H 'Content-Type: application/json' \\
                 -d '{"status":"success","summary":"..."}'`
              This is never required; the file alone is enough — so when this call fails, the
              work is already reported and there is nothing to repair.
              """
        let validatorLoader = #"eval("(async()=>{"+Buffer.from(process.argv[1],"base64").toString("utf8")+"\n})()")"#
        let resultTmp = dir + "/result.json.tmp"
        let resultFile = dir + "/result.json"
        let resultReady = dir + "/result.json.ready"
        let resultPreflightCommand = "node -e " + Project.shellQuoted(validatorLoader) + " "
            + Project.shellQuoted(childResultValidatorProgramBase64) + " "
            + Project.shellQuoted(dir + "/task.json") + " " + Project.shellQuoted(resultTmp) + " "
            + Project.shellQuoted(resultReady)
            + " && mv -- " + Project.shellQuoted(resultTmp) + " " + Project.shellQuoted(resultFile)
            + " && rm -f -- " + Project.shellQuoted(resultReady)
        let reviewReporting = typedReviewReporting(for: task)
        return """
        \(briefingHeading)

        \(ProjectBoardIntegration.workflowContext(itemID: task.workItemID, phase: task.workPhase))

        \(ownershipIntro)
        \(planningSection(for: task))\(attachedSection)
        ## Language, and the first thing you say

        The person watching this terminal reads \(languageName). Everything you say in this
        session, and the "summary" you write into result.json, is in that language — this
        briefing is in English only so that every assistant reads it the same way.

        Before you read task.json or touch anything, say exactly this line, on its own:

        \(L.t.childAnnounce(task.title))

        Then, once you have read task.json, one more line in the same language saying in your
        own words what you are about to do and where the output will go.

        ## Rules

        \(workspaceRule)
        \(scratchRule(for: task, dir: dir))
        - \(handOnRule)
        - Do not read any directory under /tmp/.clawdline/ except your own and any your
          instructions name explicitly. That second one is how a reviewing node works: it is sent
          to read what other nodes produced, so its instructions list those paths.
        - \(landingRule)
        - Do not do work the task did not ask for.
        - You have \(task.timeoutMinutes) minutes before the task is marked timed out.\(isolationSection)\(houseRules)

        ## Verification budget

        `./build.sh` is forbidden. Do not use an app restart or clicking the real UI as acceptance,
        re-run a full suite as a ritual after every small edit, run a suite unrelated to the paths this task claimed,
        or repeat a run whose only purpose is to see whether something is flaky.

        Do the cheapest verification pass that materially reduces the risk of the complete delivery
        unit. Accumulate related edits first, then compile and run the relevant groups once near the
        end; do not pay a Swift compile for each assertion, file, finding or small correction. Use
        one representative red-before-green or failure-injection proof for each materially new
        failure class when the test could otherwise pass without the behavior. Pure prose and
        generated-count transcription do not need a synthetic mutation. Mechanical moves and
        test-fixture-only corrections do not need one either. Until the repository ships a
        focused Swift runner, an implementer whose behavior cannot be exercised more narrowly may
        use one full-suite run and record `focused_runner_unavailable`; a reviewer does not repeat
        it.

        **An expensive compile goes through the machine lock, and there is exactly one slot.**
        Four `swift-frontend` processes have force-rebooted this Mac. `./test.sh` and `./build.sh`
        take the slot for themselves, so ordinarily you do nothing but run them: a run that waits
        is queueing, not stuck, and it prints who holds the slot and for how long.

        **Do not start `swiftc` by hand and do not work around a wait.** Compiling outside the slot
        is the thing that rebooted the machine. If your task genuinely cannot be verified any other
        way, say so in `result.json` rather than compiling around the queue — a blocked verification
        reported honestly costs this machine nothing, and a second compiler costs it everything.
        `CLAWDLINE_SUITE_JOBS=<n>` is the supported way to ask `./test.sh` for fewer compiler jobs.

        Nothing in this system will ever end somebody else's compile, and neither may you.

        Verification stops after one third of this task's timeout
        (\(verificationMinutes) minutes). At the limit, stop and report the state reached in `result.json`.
        \(verificationScratchRule(for: task, dir: dir))

        \(timelySection)

        ## Report only a material boundary change

        Do not echo a clear `task.json` back as a routine progress message and do not send
        heartbeat status. Start the work. Send one short progress note only when you discover that
        the write set, approach, dependency, risk, or blocker materially differs from the briefing,
        or when a long-running task needs an early choice from its root. Say what changed:

        \(progressChannel)

        **This is not a status feed.** Ordinary progress and results stay in your final
        `result.json`. The newest \(progressKept) material changes are kept; sending the same
        sentence twice is ignored rather than refused.

        \(inflightSection)

        ## Reporting — this is the completion signal, do it exactly

        When the work is done (or has failed for good), first write \(resultTmp):

        ```json
        {"clawdline_protocol": 1,
         "task_id": "\(task.id)",
         "task_secret": "<the TASK_SECRET value from your first message>",
         "status": "success",
         "summary": "<one paragraph: what you did, or why it failed>",
         "symbols": ["<every name your change introduced>", "..."],
         "artifacts": ["artifacts/<file>", "..."],
         "verification": {"runs": 2, "seconds": 940, "last": "pass", "scope": "swift suite + web-schedules"},
         "finished_at": "<ISO8601 UTC>"}
        ```

        Use "status": "failure" when you could not do it. Then run this exact preflight and
        atomic rename command:

        ```bash
        \(resultPreflightCommand)
        ```

        The validator is carried inside this briefing, so this works even when the project you
        are working in has no Clawdline checkout or `tools/` directory. It validates the task and
        result identities, status, optional verification, and the closed review receipt when one
        is present or required. It never prints the `task_secret` value. If validation fails, do
        not rename or delete the tmp file: correct that file and run the same command again. `result.json`
        remains the only completion signal; the task is considered finished only after the
        successful rename creates it.
        \(reviewReporting)

        **`symbols` is how your work is told apart from everybody else's.** This tree is shared:
        by the time root commits, the files you edited may hold two or three sessions' unfinished
        work, and root separates them by looking for vocabulary. Guessing that vocabulary is
        error-prone — root has staged trees that would not compile because a hunk *reading* like
        yours actually called somebody else's new function. So list what you introduced: new
        functions and types, new fields, new string keys, the names of test groups you added.
        Names, not descriptions. A wrong or missing list costs somebody an hour; it costs you a
        minute.

        **If you gave part of this to your own subagents and it did not come back, say so in the
        summary.** Doing it yourself instead is usually right — the answer is what was asked for,
        not who produced it. What is not right is a summary that reads as though those subagents
        did the work when they never finished. Whoever reads this is deciding how much to trust
        the result, and "both halves came back" and "both halves failed and I did it myself" are
        different amounts of evidence behind the same answer.

        **Write the tmp file with your file-writing tool, not with a shell command.** A shell line
        that builds JSON gets refused by command screening on its own shape — quotes inside braces,
        a redirect it cannot analyse statically — and that refusal is a prompt with no "always
        allow" on a tab nobody is watching. The exact command above is the one shell step: its
        guarded rename supplies atomicity, and a half-written tmp file is never a completion signal.

        \(announceSection)
        """
    }
}
