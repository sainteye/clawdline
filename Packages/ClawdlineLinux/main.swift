import Foundation

do {
    let output = try LinuxComposition.execute(arguments: Array(CommandLine.arguments.dropFirst()))
    FileHandle.standardOutput.write(output)
} catch let error as LinuxCompositionError {
    FileHandle.standardError.write(LinuxComposition.errorData(error))
    switch error {
    case .badArguments:
        exit(64)
    case .configuration, .secret:
        exit(78)
    case .runtimeUnavailable:
        exit(69)
    case .internalFailure:
        exit(70)
    }
} catch {
    FileHandle.standardError.write(LinuxComposition.errorData(.internalFailure))
    exit(70)
}
