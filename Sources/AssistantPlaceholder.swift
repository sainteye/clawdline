import Foundation

extension Assistant {
    /// The translations predate multiple assistants and therefore contain Claude's product
    /// name. Product names are not translated, so substituting the one dynamic noun preserves
    /// the surrounding fourteen translations without maintaining fourteen parallel strings.
    func promptPlaceholder(from localizedClaudePlaceholder: String) -> String {
        guard self == .codex else { return localizedClaudePlaceholder }
        let changed = localizedClaudePlaceholder
            .replacingOccurrences(of: "Claude Code", with: label)
            .replacingOccurrences(of: "Claude", with: label)
        return changed == localizedClaudePlaceholder ? label + "…" : changed
    }

    /// Claude Code invokes a skill as `/name`; Codex invokes one as `$name`.
    var skillInvocationPrefix: String { self == .codex ? "$" : "/" }
}
