import Foundation
import ClawdlineApplication

/// Ubuntu's production ownership leaf for the shared Application ledger and spool. The daemon
/// opens both stores before local ingress is admitted and retains their writer locks for its whole
/// lifetime. Cloud networking/authentication remains unready in W5-1, so this type exposes no
/// publish door and cannot accidentally promote the W0-E candidate into production authority.
final class LinuxDurableCloudRuntime {
    struct Readiness: Codable, Equatable {
        let durable: Bool
        let authority: String
        let cutoverRequired: Bool
        let emissionEnabled: Bool
        let code: String
    }

    let runtime: CloudDurableRuntime
    let candidateAuthority = CloudContractCandidateAuthority.w0E

    init(stateDirectory: String, expectedUID: UInt32) throws {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(stateDirectory) else {
            throw LinuxDurableStateFailure(
                code: "unsafe_cloud_state_path",
                message: "The Cloud durable state path must be canonical and absolute.")
        }
        runtime = try CloudDurableRuntime.open(
            directory: URL(fileURLWithPath: stateDirectory, isDirectory: true)
                .appendingPathComponent("cloud", isDirectory: true),
            // W0-E accepts only mac|viewer. Linux metrics therefore remain typed-unavailable
            // until an additive accepted label exists; reporting Ubuntu as Mac is forbidden.
            expectedUID: expectedUID, runtime: nil,
            strictPersistedFrameValidation: true)
    }

    var readiness: Readiness {
        Readiness(
            durable: true,
            authority: candidateAuthority.authority,
            cutoverRequired: candidateAuthority.cutoverRequired,
            emissionEnabled: candidateAuthority.permitsEmission(wireVersion: 1),
            code: "w5_cloud_authentication_and_cutover_not_proven")
    }
}
