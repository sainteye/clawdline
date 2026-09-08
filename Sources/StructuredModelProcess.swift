import Darwin
import Foundation

/// A model subprocess has a bounded stdin file and a capped in-memory stdout stream.
/// Its timeout includes launch and all I/O; descendants share an owned process group.
enum StructuredModelProcess {
    static func run(executable: URL, arguments: [String], environment: [String: String],
                    input: Data, maximumOutputBytes: Int, timeout: TimeInterval,
                    directory: URL? = nil, shouldStart: () -> Bool = { true }) -> String? {
        guard maximumOutputBytes > 0, maximumOutputBytes <= 1_048_576,
              input.count <= 32_768, timeout > 0, timeout <= 60 else { return nil }
        let scratch = FileManager.default.temporaryDirectory.appendingPathComponent(
            "clawdline-model-io-\(UUID().uuidString)", isDirectory: true)
        let stdinFile = scratch.appendingPathComponent("input")
        do {
            try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700])
            try input.write(to: stdinFile)
        } catch { return nil }
        defer { try? FileManager.default.removeItem(at: scratch) }

        var descriptors: [Int32] = [0, 0]
        guard pipe(&descriptors) == 0 else { return nil }
        let reader = descriptors[0], writer = descriptors[1]
        defer { close(reader) }
        var actions: posix_spawn_file_actions_t?
        var attributes: posix_spawnattr_t?
        guard posix_spawn_file_actions_init(&actions) == 0 else { close(writer); return nil }
        defer { posix_spawn_file_actions_destroy(&actions) }
        guard posix_spawnattr_init(&attributes) == 0 else { close(writer); return nil }
        defer { posix_spawnattr_destroy(&attributes) }
        var setup = posix_spawnattr_setflags(&attributes,
            Int16(POSIX_SPAWN_SETPGROUP | POSIX_SPAWN_CLOEXEC_DEFAULT))
        setup |= posix_spawnattr_setpgroup(&attributes, 0)
        setup |= posix_spawn_file_actions_addopen(&actions, STDIN_FILENO, stdinFile.path, O_RDONLY, 0)
        setup |= posix_spawn_file_actions_adddup2(&actions, writer, STDOUT_FILENO)
        setup |= posix_spawn_file_actions_addopen(&actions, STDERR_FILENO, "/dev/null", O_WRONLY, 0)
        if let directory { setup |= posix_spawn_file_actions_addchdir_np(&actions, directory.path) }
        guard setup == 0 else { close(writer); return nil }
        let argv = ([executable.path] + arguments).map { strdup($0) } + [nil]
        let envp = environment.map { strdup("\($0.key)=\($0.value)") } + [nil]
        defer { argv.forEach { free($0) }; envp.forEach { free($0) } }
        guard shouldStart() else { close(writer); return nil }
        let deadline = ProcessInfo.processInfo.systemUptime + timeout
        var pid: pid_t = 0
        let spawned = argv.withUnsafeBufferPointer { args in
            envp.withUnsafeBufferPointer { env in
                posix_spawn(&pid, executable.path, &actions, &attributes,
                    UnsafeMutablePointer(mutating: args.baseAddress!),
                    UnsafeMutablePointer(mutating: env.baseAddress!))
            }
        }
        close(writer)
        guard spawned == 0 else { return nil }
        var status: Int32 = 0, reaped = false
        defer {
            // Only the process group created by this invocation is targeted.
            kill(-pid, SIGKILL)
            if !reaped { while waitpid(pid, &status, 0) < 0 && errno == EINTR {} }
        }
        guard fcntl(reader, F_SETFL, O_NONBLOCK) == 0 else { return nil }
        var output = Data(), buffer = [UInt8](repeating: 0, count: 4096)
        while ProcessInfo.processInfo.systemUptime < deadline {
            let count = read(reader, &buffer, buffer.count)
            if count > 0 {
                guard output.count + count <= maximumOutputBytes else { return nil }
                output.append(contentsOf: buffer.prefix(count))
                continue
            }
            if count < 0 && errno != EAGAIN && errno != EINTR { return nil }
            if !reaped {
                let result = waitpid(pid, &status, WNOHANG)
                if result == pid { reaped = true }
                else if result < 0 && errno != EINTR { return nil }
            }
            if reaped && count == 0 {
                return status == 0 ? String(data: output, encoding: .utf8) : nil
            }
            Thread.sleep(forTimeInterval: 0.01)
        }
        return nil
    }
}
