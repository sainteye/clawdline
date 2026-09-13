import Foundation

/// A deliberately small newline-delimited JSON control plane. It binds only the already-validated
/// loopback address, accepts one bounded request per connection, authenticates before dispatch,
/// and sends a response only after the durable owner has persisted its receipt.
final class LinuxLocalIngressServer {
    static let maximumRequestBytes = 3 * 1_048_576

    private let configuration: LinuxListenConfiguration
    private let authorization: Data
    private let owner: LinuxDaemonIngressOwner
    private let relaySupervisor: LinuxRelayRuntimeSupervisor?

    init(configuration: LinuxListenConfiguration, authorization: Data,
         owner: LinuxDaemonIngressOwner,
         relaySupervisor: LinuxRelayRuntimeSupervisor? = nil) {
        self.configuration = configuration
        self.authorization = authorization
        self.owner = owner
        self.relaySupervisor = relaySupervisor
    }

    func run() throws -> Never {
        let descriptor = try makeListener()
        defer {
            _ = close(descriptor)
            relaySupervisor?.stopAndWait()
        }
        while true {
            let client = accept(descriptor, nil, nil)
            if client < 0 {
                if errno == EINTR { continue }
                throw LinuxDurableStateFailure(code: "ingress_accept_failed",
                                               message: "The local ingress listener failed.")
            }
            handle(client)
        }
    }

    private func handle(_ client: Int32) {
        defer { _ = close(client) }
        _ = fcntl(client, F_SETFD, FD_CLOEXEC)
        var timeout = timeval(tv_sec: 5, tv_usec: 0)
        _ = setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout,
                       socklen_t(MemoryLayout.size(ofValue: timeout)))
        _ = setsockopt(client, SOL_SOCKET, SO_SNDTIMEO, &timeout,
                       socklen_t(MemoryLayout.size(ofValue: timeout)))
        do {
            let requestBytes = try readRequest(client)
            guard let object = try JSONSerialization.jsonObject(with: requestBytes)
                    as? [String: Any],
                  Set(object.keys).isSubset(of: Self.requestFields) else {
                throw LinuxDurableStateFailure(code: "invalid_ingress",
                                               message: "The local request has an unknown field.")
            }
            let request = try JSONDecoder().decode(LinuxIngressRequest.self, from: requestBytes)
            guard let supplied = request.authorization.flatMap({ Data(base64Encoded: $0) }),
                  constantTimeEqual(supplied, authorization) else {
                throw LinuxDurableStateFailure(code: "ingress_unauthorized",
                                               message: "Local ingress authentication failed.")
            }
            var response = try owner.perform(request)
            response.append(0x0a)
            try writeAll(response, to: client)
        } catch {
            let failure: LinuxDurableStateFailure
            if let typed = error as? LinuxDurableStateFailure { failure = typed }
            else {
                failure = LinuxDurableStateFailure(code: "ingress_failed",
                                                   message: "The local ingress request failed closed.")
            }
            let envelope = LinuxErrorEnvelope(error: .init(code: failure.code,
                                                            message: failure.message))
            if var bytes = try? JSONEncoder().encode(envelope) {
                bytes.append(0x0a)
                try? writeAll(bytes, to: client)
            }
        }
    }

    private static let requestFields: Set<String> = [
        "authorization", "operation", "commandID", "taskID", "sessionID",
        "projectRoot", "assistant", "text", "acknowledge", "authorizeRecovery",
        "taskSecret", "title", "claims", "resultBase64", "resultDigest",
        "documentScope", "relativePath",
    ]

    private func readRequest(_ descriptor: Int32) throws -> Data {
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 16_384)
        while data.count <= Self.maximumRequestBytes {
            let count = buffer.withUnsafeMutableBytes {
                read(descriptor, $0.baseAddress, $0.count)
            }
            if count == 0 { break }
            if count < 0 {
                if errno == EINTR { continue }
                throw LinuxDurableStateFailure(code: "ingress_read_failed",
                                               message: "The local request could not be read.")
            }
            data.append(contentsOf: buffer.prefix(count))
            if let newline = data.firstIndex(of: 0x0a) {
                data = data.prefix(upTo: newline)
                break
            }
        }
        guard !data.isEmpty, data.count <= Self.maximumRequestBytes else {
            throw LinuxDurableStateFailure(code: "ingress_request_limit",
                                           message: "The local request is empty or too large.")
        }
        return data
    }

    private func writeAll(_ data: Data, to descriptor: Int32) throws {
        var offset = 0
        try data.withUnsafeBytes { bytes in
            while offset < bytes.count {
                let count = write(descriptor, bytes.baseAddress!.advanced(by: offset),
                                  bytes.count - offset)
                if count < 0, errno == EINTR { continue }
                guard count > 0 else {
                    throw LinuxDurableStateFailure(code: "ingress_write_failed",
                                                   message: "The local response could not be written.")
                }
                offset += count
            }
        }
    }

    private func constantTimeEqual(_ lhs: Data, _ rhs: Data) -> Bool {
        var difference = lhs.count ^ rhs.count
        let count = max(lhs.count, rhs.count)
        for index in 0..<count {
            let a = index < lhs.count ? lhs[index] : 0
            let b = index < rhs.count ? rhs[index] : 0
            difference |= Int(a ^ b)
        }
        return difference == 0
    }

    private func makeListener() throws -> Int32 {
        #if os(Linux)
        let descriptor = socket(configuration.host == "::1" ? AF_INET6 : AF_INET,
                                Int32(SOCK_STREAM.rawValue), 0)
        #else
        let descriptor = socket(configuration.host == "::1" ? AF_INET6 : AF_INET,
                                SOCK_STREAM, 0)
        #endif
        guard descriptor >= 0 else { return try listenerFailure() }
        var reuse: Int32 = 1
        guard setsockopt(descriptor, SOL_SOCKET, SO_REUSEADDR, &reuse,
                         socklen_t(MemoryLayout.size(ofValue: reuse))) == 0 else {
            _ = close(descriptor)
            return try listenerFailure()
        }
        let bound: Int32
        if configuration.host == "::1" {
            var address = sockaddr_in6()
            address.sin6_family = sa_family_t(AF_INET6)
            address.sin6_port = in_port_t(UInt16(configuration.port).bigEndian)
            let converted = configuration.host.withCString {
                inet_pton(AF_INET6, $0, &address.sin6_addr)
            }
            guard converted == 1 else { _ = close(descriptor); return try listenerFailure() }
            bound = withUnsafePointer(to: &address) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                    bind(descriptor, $0, socklen_t(MemoryLayout<sockaddr_in6>.size))
                }
            }
        } else {
            var address = sockaddr_in()
            address.sin_family = sa_family_t(AF_INET)
            address.sin_port = in_port_t(UInt16(configuration.port).bigEndian)
            let converted = configuration.host.withCString {
                inet_pton(AF_INET, $0, &address.sin_addr)
            }
            guard converted == 1 else { _ = close(descriptor); return try listenerFailure() }
            bound = withUnsafePointer(to: &address) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                    bind(descriptor, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
                }
            }
        }
        guard bound == 0, listen(descriptor, 16) == 0 else {
            _ = close(descriptor)
            return try listenerFailure()
        }
        return descriptor
    }

    private func listenerFailure<T>() throws -> T {
        throw LinuxDurableStateFailure(code: "ingress_listen_failed",
                                       message: "The configured loopback listener could not start.")
    }
}
