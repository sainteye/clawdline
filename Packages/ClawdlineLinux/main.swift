import Foundation

let linuxArguments = Array(CommandLine.arguments.dropFirst())
if linuxArguments.count == 2, linuxArguments[0] == LinuxProviderSandbox.command {
    LinuxProviderSandbox.launch(encodedSpec: linuxArguments[1])
}

do {
    let output = try LinuxComposition.execute(arguments: linuxArguments)
    FileHandle.standardOutput.write(output)
} catch let error as LinuxCompositionError {
    FileHandle.standardError.write(LinuxComposition.errorData(error))
    switch error {
    case .badArguments:
        exit(64)
    case .configuration, .secret:
        exit(78)
    case .runtime:
        exit(69)
    case .internalFailure:
        exit(70)
    }
} catch {
    FileHandle.standardError.write(LinuxComposition.errorData(.internalFailure))
    exit(70)
}
