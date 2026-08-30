import Foundation

/// Pure decisions shared by Session closeability, startup-menu recovery and terminal cleanup.
///
/// The inputs are already-observed facts. Terminal reads, task storage, queue admission and
/// keystrokes stay with their adapters; this type only says which evidence domains can affect one
/// another and which reversible next step those facts permit.
enum SessionClosePolicy {
    enum MenuStep: Equatable {
        case none
        case answer(row: Int)
        case leaveToOwner
    }

    /// Claude Code 2.1.228 moved the rejecting choice to the selected first row and hid its
    /// numbers. Identify the affirmative and exit rows by the two-sided vocabulary instead of
    /// assuming either order. A future startup picker with different semantics is left alone.
    static func startupTrustChoices(in menu: SessionState.Menu)
        -> (accept: Int, exit: Int)? {
        guard menu.options.count == 2 else { return nil }
        func words(_ label: String) -> Set<String> {
            Set(label.lowercased().split { !$0.isLetter }.map(String.init))
        }
        let classified = menu.options.map { option -> (number: Int, positive: Bool, exit: Bool) in
            let tokens = words(option.label)
            return (option.number,
                    !tokens.isDisjoint(with: ["yes", "trust", "continue"]),
                    !tokens.isDisjoint(with: ["no", "exit", "quit"]))
        }
        let accepts = classified.filter { $0.positive && !$0.exit }
        let exits = classified.filter { $0.exit && !$0.positive }
        guard accepts.count == 1, exits.count == 1,
              accepts[0].number != exits[0].number else { return nil }
        return (accepts[0].number, exits[0].number)
    }

    static func menuStep(answered: Bool, attached: Bool,
                         menu: SessionState.Menu?) -> MenuStep {
        guard let menu, !answered else { return .none }
        guard !attached else { return .leaveToOwner }
        guard let choices = startupTrustChoices(in: menu) else { return .none }
        return .answer(row: choices.accept)
    }

    /// Missing conversation evidence can hide a duplicate only inside the provider namespace in
    /// which conversation ids are compared. A row whose provider is itself unreadable still
    /// affects every namespace because there is no safe bucket for it.
    static func identityMatchCounts(_ identities: [Orchestrator.SessionWorkIdentity])
        -> [String: Int] {
        let hasUnclassifiedAssistant = identities.contains { $0.assistant == nil }
        let assistantsWithUnreadableConversation = Set(identities.compactMap { identity in
            identity.conversationID == nil ? identity.assistant : nil
        })
        var byConversation: [String: Int] = [:]
        for identity in identities {
            guard let assistant = identity.assistant,
                  let conversation = identity.conversationID else { continue }
            let key = assistant.rawValue + "\u{1}" + conversation
            byConversation[key] = (byConversation[key] ?? 0) + 1
        }
        var out: [String: Int] = [:]
        for identity in identities {
            guard let assistant = identity.assistant,
                  let conversation = identity.conversationID else {
                out[identity.terminalID] = 1
                continue
            }
            if hasUnclassifiedAssistant
                || assistantsWithUnreadableConversation.contains(assistant) {
                out[identity.terminalID] = 0
            } else {
                out[identity.terminalID] =
                    byConversation[assistant.rawValue + "\u{1}" + conversation] ?? 1
            }
        }
        return out
    }

    enum CloseStep: Equatable {
        case wait
        case forget
        case dismissStartupMenu(row: Int)
        case close(justTheTab: Bool)
    }

    static func closeStep(now: Date, closeAt: Date, inventoryComplete: Bool,
                          inventoryEmpty: Bool, emptyInventoryAuthoritative: Bool,
                          automationReady: Bool, retryAllowed: Bool,
                          child: TargetSession?, assistant: Assistant, tty: String?,
                          startupMenuExitRow: Int?,
                          activity: () -> Targets.SafeCloseActivity) -> CloseStep {
        guard now >= closeAt, inventoryComplete, automationReady, retryAllowed else { return .wait }
        guard let child else {
            return !inventoryEmpty || emptyInventoryAuthoritative ? .forget : .wait
        }
        guard child.assistant == nil || child.assistant == assistant,
              tty == nil || child.tty == tty else { return .forget }
        if let startupMenuExitRow { return .dismissStartupMenu(row: startupMenuExitRow) }
        guard activity() == .idle else { return .wait }
        return .close(justTheTab: child.assistant == nil)
    }

    static func failedSpawnStartupExitRow(
        failed: Bool, wasSpokenTo: Bool, expectedTerminalID: String?, expectedTTY: String?,
        expectedAssistant: Assistant, expectedPID: Int32?, expectedStart: Date?,
        child: TargetSession, currentPID: Int32?, currentStart: Date?, screen: String?
    ) -> Int? {
        guard failed, !wasSpokenTo, expectedTerminalID == child.id, expectedTTY == child.tty,
              expectedAssistant == child.assistant,
              let expectedPID, expectedPID == currentPID,
              let expectedStart, let currentStart,
              abs(expectedStart.timeIntervalSince(currentStart)) <= SessionRegistry.startTolerance,
              let screen,
              let menu = SessionState.menu(screen, assistant: expectedAssistant, hookWaiting: true),
              let choices = startupTrustChoices(in: menu) else { return nil }
        return choices.exit
    }
}
