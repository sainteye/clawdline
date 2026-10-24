// The pairing alerts. Each screen is built apart from running it, so the shell
// and a preview that drives the same flow put up the very same alerts.
import AppKit

enum PairingDialogs {
    /// The screens a pairing puts up, as PairingPermissionFlow asks for them.
    static var screens: PairingScreens {
        PairingScreens(
            choose: { notice in choose(notice) },
            showCode: { notice, permission in showCode(notice, permission) },
            showFailure: { notice, outcome in showFailure(notice, outcome) })
    }

    static func alert(_ notice: PairingNotice) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = L.t.pairingAsks(notice.name)
        alert.alertStyle = .informational
        return alert
    }

    /// Operate is the first button, as the daemon's default is; Escape ignores.
    static func chooseAlert(_ notice: PairingNotice) -> NSAlert {
        let alert = alert(notice)
        alert.informativeText = L.t.pairingChooseBody
        alert.addButton(withTitle: L.t.pairingReadAndOperate)
        alert.addButton(withTitle: L.t.pairingReadOnly)
        alert.addButton(withTitle: L.t.pairingIgnore).keyEquivalent = "\u{1b}"
        return alert
    }

    static func codeAlert(_ notice: PairingNotice, _ permission: PairingPermission) -> NSAlert {
        let alert = alert(notice)
        alert.informativeText = L.t.pairingSaved(permission) + "\n\n" + L.t.pairingCode(notice.code)
        alert.addButton(withTitle: L.t.pairingIgnore)
        return alert
    }

    /// An expired pairing has nothing to retry: one button. Anything else
    /// offers to try again first, and Escape ignores.
    static func failureAlert(_ notice: PairingNotice, _ outcome: PairingPermissionOutcome) -> NSAlert {
        let alert = alert(notice)
        alert.alertStyle = .warning
        alert.informativeText = L.t.pairingFailed(outcome)
        if outcome == .expired {
            alert.addButton(withTitle: L.t.dialogOK)
        } else {
            alert.addButton(withTitle: L.t.pairingRetry)
            alert.addButton(withTitle: L.t.pairingIgnore).keyEquivalent = "\u{1b}"
        }
        return alert
    }

    static func choose(_ notice: PairingNotice) -> PairingChoice {
        NSApp.activate(ignoringOtherApps: true)
        switch chooseAlert(notice).runModal() {
        case .alertFirstButtonReturn: return .grant(.readAndOperate)
        case .alertSecondButtonReturn: return .grant(.readOnly)
        default: return .ignore
        }
    }

    static func showCode(_ notice: PairingNotice, _ permission: PairingPermission) {
        NSApp.activate(ignoringOtherApps: true)
        codeAlert(notice, permission).runModal()
    }

    /// True to try again.
    static func showFailure(_ notice: PairingNotice, _ outcome: PairingPermissionOutcome) -> Bool {
        NSApp.activate(ignoringOtherApps: true)
        let answer = failureAlert(notice, outcome).runModal()
        return outcome != .expired && answer == .alertFirstButtonReturn
    }
}
