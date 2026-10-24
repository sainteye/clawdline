// Native menu, title, status and alert copy. The browser's validated UI catalog
// selects the language; agent, voice and daemon product languages are separate.
import Foundation

enum L {
    static var t: ShellCopy { ShellCopy() }
}

struct ShellCopy {
    private func word(_ key: String, _ values: [String] = []) -> String {
        ShellLanguage.word(key, values)
    }

    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? word("statusWaitingOne", [labels[0]])
                          : word("statusWaitingMany", [String(labels.count)])
    }
    func statusWorking(_ count: Int) -> String { word("statusWorking", [String(count)]) }

    var islandDone: String { word("islandDone") }
    var islandAllSessions: String { word("islandAllSessions") }
    var menuApplication: String { "Clawdline" }
    var menuWindow: String { word("menuWindow") }
    var menuHelp: String { word("menuHelp") }
    var menuClose: String { word("menuClose") }
    var menuHome: String { word("menuHome") }
    var menuHelpDocumentation: String { word("menuHelpDocumentation") }
    var menuHelpSetup: String { word("menuHelpSetup") }
    func menuAbout(_ app: String) -> String { word("menuAbout", [app]) }
    var menuServices: String { word("menuServices") }
    func menuHide(_ app: String) -> String { word("menuHide", [app]) }
    var menuHideOthers: String { word("menuHideOthers") }
    var menuShowAll: String { word("menuShowAll") }
    var menuMinimize: String { word("menuMinimize") }
    var menuOpen: String { word("menuOpen") }
    var menuReveal: String { word("menuReveal") }
    var menuMascot: String { word("menuMascot") }
    var menuLogin: String { word("menuLogin") }
    var menuEditConfig: String { word("menuEditConfig") }
    var menuEdit: String { word("menuEdit") }
    var menuUndo: String { word("menuUndo") }
    var menuCut: String { word("menuCut") }
    var menuCopy: String { word("menuCopy") }
    var menuPaste: String { word("menuPaste") }
    var menuSelectAll: String { word("menuSelectAll") }
    var pairInvitationAsks: String { word("pairInvitationAsks") }
    var pairInvitationBody: String { word("pairInvitationBody") }
    var pairLinkDone: String { word("pairLinkDone") }
    var pairLinkReturn: String { word("pairLinkReturn") }
    var pairLinkFailed: String { word("pairLinkFailed") }
    var pairLinkRetry: String { word("pairLinkRetry") }
    func cloudConnectAsks(_ machine: String) -> String { word("cloudConnectAsks", [machine]) }
    var cloudConnectBody: String { word("cloudConnectBody") }
    var cloudConnectStart: String { word("cloudConnectStart") }
    func pairMachineFingerprint(_ fingerprint: String) -> String { word("pairMachineFingerprint", [fingerprint]) }
    func pairBrowserFingerprint(_ fingerprint: String) -> String { word("pairBrowserFingerprint", [fingerprint]) }
    var pairWrongMachine: String { word("pairWrongMachine") }
    var pairNeedsCloud: String { word("pairNeedsCloud") }
    var pairCodeExpired: String { word("pairCodeExpired") }
    var pairWrongAccount: String { word("pairWrongAccount") }
    var pairUnavailable: String { word("pairUnavailable") }
    var menuReload: String { word("menuReload") }
    var menuQuit: String { word("menuQuit") }
    var menuNoTarget: String { word("menuNoTarget") }

    func dictationStatus(_ status: DictationEngine.Status) -> String {
        switch status {
        case .ready(let model): return word("dictationReady", [model])
        case .noBinary: return word("dictationNoBinary")
        case .noModel: return word("dictationNoModel")
        }
    }
    func updateAvailable(latest: String, installed: String) -> String {
        word("updateAvailable", [latest, installed])
    }
    func compatNote(_ standing: Compat.Standing) -> String {
        switch standing {
        case .behind(let program, let installed, let builtAgainst):
            return word("compatBehind", [program, installed, builtAgainst])
        case .ahead(let program, let installed, let builtAgainst, let release):
            return word("compatAhead", [program, installed, builtAgainst, release])
        }
    }
    func hotkeyFailedTitle(_ combo: String) -> String { word("hotkeyFailedTitle", [combo]) }
    func hotkeyFailedBody(system: Bool) -> String {
        word(system ? "hotkeyFailedSystem" : "hotkeyFailedBody")
    }
    var loginFailed: String { word("loginFailed") }

    var settingsTitle: String { word("settingsTitle") }
    var settingsHotkey: String { word("settingsHotkey") }
    var settingsRecording: String { word("settingsRecording") }
    var settingsScope: String { word("settingsScope") }
    var settingsScopeGlobal: String { word("settingsScopeGlobal") }
    var settingsOff: String { word("settingsOff") }

    var homeLocalTitle: String { word("homeLocalTitle") }
    var browserCloud: String { "Cloud" }
    var webInfoRefresh: String { word("webInfoRefresh") }
    var settingsRemoteOpen: String { word("settingsRemoteOpen") }
    var browserBack: String { word("browserBack") }
    var browserForward: String { word("browserForward") }
    var menuView: String { word("menuView") }
    var menuActualSize: String { word("menuActualSize") }
    var menuZoomIn: String { word("menuZoomIn") }
    var menuZoomOut: String { word("menuZoomOut") }
    func zoomLevel(_ percent: Int) -> String { word("zoomLevel", [String(percent)]) }
    func zoomCeiling(_ percent: Int) -> String { word("zoomCeiling", [String(percent)]) }
    func zoomFloor(_ percent: Int) -> String { word("zoomFloor", [String(percent)]) }
    var browserShowCloud: String { word("browserShowCloud") }
    var dialogOK: String { word("dialogOK") }
    var dialogCancel: String { word("dialogCancel") }
    func pairLocalAsks(_ machine: String) -> String { word("pairLocalAsks", [machine]) }
    var pairLocalBody: String { word("pairLocalBody") }
    var pairLocalStart: String { word("pairLocalStart") }
    func pairAgentAsks(_ machine: String) -> String { word("pairAgentAsks", [machine]) }
    func pairAgentCommand(_ offer: String) -> String { word("pairAgentCommand", [offer]) }
    var pairAgentStart: String { word("pairAgentStart") }
    var pairingIgnore: String { word("pairingIgnore") }
    func pairingAsks(_ device: String) -> String { word("pairingAsks", [device]) }
    func pairingCode(_ code: String) -> String { word("pairingCode", [code]) }
    var pairingChooseBody: String { word("pairingChooseBody") }
    var pairingReadOnly: String { word("pairingReadOnly") }
    var pairingReadAndOperate: String { word("pairingReadAndOperate") }
    var pairingRetry: String { word("pairingRetry") }
    func pairingSaved(_ permission: PairingPermission) -> String {
        switch permission {
        case .readOnly: return word("pairingSavedReadOnly")
        case .readAndOperate: return word("pairingSavedReadAndOperate")
        }
    }
    func pairingFailed(_ outcome: PairingPermissionOutcome) -> String {
        switch outcome {
        case .expired: return word("pairingFailedExpired")
        case .refused(let status): return word("pairingFailedRefused", [String(status)])
        case .unreachable, .saved: return word("pairingFailedUnreachable")
        }
    }
    var daemonUnavailableTitle: String { word("daemonUnavailableTitle") }
    func daemonUnavailableBody(_ address: String) -> String { word("daemonUnavailableBody", [address]) }
    var daemonUnavailableStart: String { word("daemonUnavailableStart") }
    var daemonUnavailableRetrying: String { word("daemonUnavailableRetrying") }
    var daemonUnavailableStopped: String { word("daemonUnavailableStopped") }
}
