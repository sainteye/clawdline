import Crypto
import Dispatch
import Foundation
import FoundationNetworking
import Glibc

private struct ProbeFailure: Error, CustomStringConvertible {
    let description: String
}

private final class InjectedTime {
    var wall: TimeInterval = 1_700_000_000
    var continuous: TimeInterval = 10_000
    var bootID = "boot-a"

    var clock: CloudClock {
        CloudClock(
            wall: { [unowned self] in Date(timeIntervalSince1970: self.wall) },
            continuous: { [unowned self] in self.continuous },
            bootID: { [unowned self] in self.bootID }
        )
    }

    var wallDate: Date { Date(timeIntervalSince1970: wall) }

    func advance(wall wallDelta: TimeInterval, continuous continuousDelta: TimeInterval) {
        wall += wallDelta
        continuous += continuousDelta
    }
}

private func writeAll(_ bytes: [UInt8], to descriptor: Int32) throws {
    var written = 0
    while written < bytes.count {
        let count = bytes.withUnsafeBytes { rawBuffer -> Int in
            let start = rawBuffer.baseAddress!.advanced(by: written)
            return Glibc.write(descriptor, start, bytes.count - written)
        }
        guard count > 0 else {
            throw ProbeFailure(description: "loopback response write failed: errno=\(errno)")
        }
        written += count
    }
}

private final class LoopbackServerOutcome: @unchecked Sendable {
    private let lock = NSLock()
    private var storedError: Error?

    func record(_ error: Error) {
        lock.lock()
        storedError = error
        lock.unlock()
    }

    var error: Error? {
        lock.lock()
        defer { lock.unlock() }
        return storedError
    }
}

private final class LoopbackClientOutcome: @unchecked Sendable {
    private let lock = NSLock()
    private var storedData: Data?
    private var storedStatus: Int?
    private var storedError: Error?

    func record(data: Data?, response: URLResponse?, error: Error?) {
        lock.lock()
        storedData = data
        storedStatus = (response as? HTTPURLResponse)?.statusCode
        storedError = error
        lock.unlock()
    }

    var snapshot: (Data?, Int?, Error?) {
        lock.lock()
        defer { lock.unlock() }
        return (storedData, storedStatus, storedError)
    }
}

private func startLoopbackServer() throws -> (
    descriptor: Int32,
    port: UInt16,
    group: DispatchGroup,
    outcome: LoopbackServerOutcome
) {
    let descriptor = Glibc.socket(AF_INET, Int32(SOCK_STREAM.rawValue), 0)
    guard descriptor >= 0 else {
        throw ProbeFailure(description: "loopback socket failed: errno=\(errno)")
    }

    var reuse: Int32 = 1
    guard setsockopt(
        descriptor,
        SOL_SOCKET,
        SO_REUSEADDR,
        &reuse,
        socklen_t(MemoryLayout<Int32>.size)
    ) == 0 else {
        Glibc.close(descriptor)
        throw ProbeFailure(description: "loopback setsockopt failed: errno=\(errno)")
    }

    var address = sockaddr_in()
    address.sin_family = sa_family_t(AF_INET)
    address.sin_port = in_port_t(0).bigEndian
    address.sin_addr = in_addr(s_addr: inet_addr("127.0.0.1"))
    let bindStatus = withUnsafePointer(to: &address) { pointer in
        pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { socketAddress in
            Glibc.bind(descriptor, socketAddress, socklen_t(MemoryLayout<sockaddr_in>.size))
        }
    }
    guard bindStatus == 0 else {
        Glibc.close(descriptor)
        throw ProbeFailure(description: "loopback bind failed: errno=\(errno)")
    }

    var addressLength = socklen_t(MemoryLayout<sockaddr_in>.size)
    let nameStatus = withUnsafeMutablePointer(to: &address) { pointer in
        pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { socketAddress in
            Glibc.getsockname(descriptor, socketAddress, &addressLength)
        }
    }
    guard nameStatus == 0, Glibc.listen(descriptor, 1) == 0 else {
        Glibc.close(descriptor)
        throw ProbeFailure(description: "loopback listen failed: errno=\(errno)")
    }

    let group = DispatchGroup()
    let outcome = LoopbackServerOutcome()
    group.enter()
    DispatchQueue.global().async {
        defer { group.leave() }
        do {
            let client = Glibc.accept(descriptor, nil, nil)
            guard client >= 0 else {
                throw ProbeFailure(description: "loopback accept failed: errno=\(errno)")
            }
            defer { Glibc.close(client) }

            var request = [UInt8](repeating: 0, count: 4_096)
            let readCount = request.withUnsafeMutableBytes { buffer in
                Glibc.read(client, buffer.baseAddress, buffer.count)
            }
            guard readCount > 0 else {
                throw ProbeFailure(description: "loopback request read failed: errno=\(errno)")
            }

            let body = "foundation-networking-loopback"
            let response = "HTTP/1.1 200 OK\r\nContent-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)"
            try writeAll(Array(response.utf8), to: client)
        } catch {
            outcome.record(error)
        }
    }
    return (descriptor, UInt16(bigEndian: address.sin_port), group, outcome)
}

private enum UbuntuCoreProbe {
    static func run() throws -> Int {
        var checks = 0

        func check(_ condition: @autoclosure () throws -> Bool, _ name: String) throws {
            checks += 1
            guard try condition() else {
                throw ProbeFailure(description: "check \(checks) failed: \(name)")
            }
            print("ok \(checks) - \(name)")
        }

        func checkThrows(
            _ name: String,
            body: () throws -> Void,
            matches: (CloudCanonicalJSONError) -> Bool
        ) throws {
            checks += 1
            do {
                try body()
            } catch let error as CloudCanonicalJSONError {
                guard matches(error) else {
                    throw ProbeFailure(description: "check \(checks) failed: \(name) threw \(error)")
                }
                print("ok \(checks) - \(name)")
                return
            }
            throw ProbeFailure(description: "check \(checks) failed: \(name) did not throw")
        }

        let canonicalObject: CloudJSONValue = .object(["z": .int(2), "a": .int(1)])
        try check(
            String(decoding: CloudCanonicalJSON.canonicalData(canonicalObject), as: UTF8.self)
                == "{\"a\":1,\"z\":2}",
            "canonical JSON sorts object keys"
        )
        try check(
            CloudCanonicalJSON.canonicalData(.string("line\n/")).elementsEqual("\"line\\n/\"".utf8),
            "canonical JSON emits required escapes and leaves slash bare"
        )
        try check(
            try CloudCanonicalJSON.parseStrict(Data("{\"a\":1}".utf8)) == .object(["a": .int(1)]),
            "strict parser accepts canonical bytes"
        )
        try checkThrows("strict parser rejects duplicate keys", body: {
            _ = try CloudCanonicalJSON.parseStrict(Data("{\"a\":1,\"a\":2}".utf8))
        }, matches: { $0 == .duplicateKey("a") })
        try checkThrows("strict parser rejects non-canonical whitespace", body: {
            _ = try CloudCanonicalJSON.parseStrict(Data("{ \"a\":1}".utf8))
        }, matches: { $0 == .notCanonical })
        try checkThrows("strict parser rejects floating-point numbers", body: {
            _ = try CloudCanonicalJSON.parseStrict(Data("1.5".utf8))
        }, matches: { $0 == .floatingPointUnsupported("1.5") })
        try checkThrows("strict parser rejects integers outside the ECMAScript safe domain", body: {
            _ = try CloudCanonicalJSON.parseStrict(Data("9007199254740992".utf8))
        }, matches: { $0 == .integerOutOfRange("9007199254740992") })

        let signed: CloudJSONValue = .object([
            "sig": .string("top"),
            "payload": .object(["sig": .string("nested")]),
        ])
        try check(
            String(decoding: try CloudCanonicalJSON.signingInput(domain: "probe-v1", object: signed), as: UTF8.self)
                == "probe-v1\0{\"payload\":{\"sig\":\"nested\"}}",
            "signing input strips only the top-level signature"
        )
        try check(
            CloudCanonicalJSON.chargedBytes(record: canonicalObject) == 13,
            "charged bytes measures canonical UTF-8"
        )

        let digest = SHA256.hash(data: Data("abc".utf8)).map { String(format: "%02x", $0) }.joined()
        try check(
            digest == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
            "Swift Crypto SHA256 matches the published abc vector"
        )

        let time = InjectedTime()
        let guardUnderTest = EpochGuard(clock: time.clock)
        try check(
            guardUnderTest.requestAdmission == .unavailable(.stabilityPeriodIncomplete),
            "clock guard starts fail closed"
        )

        let farTime = InjectedTime()
        let farGuard = EpochGuard(clock: farTime.clock)
        try check(
            farGuard.acceptServerDate(farTime.wallDate.addingTimeInterval(301)).state
                == .uncertain(.serverSampleTooFar),
            "clock guard rejects a server sample beyond five minutes"
        )

        let accepted = guardUnderTest.acceptServerDate(time.wallDate)
        try check(
            accepted.state == .uncertain(.stabilityPeriodIncomplete) && accepted.effect == .none,
            "accepted server sample begins a fresh uncertain window"
        )
        time.advance(wall: 59, continuous: 59)
        try check(
            guardUnderTest.observe().state == .uncertain(.stabilityPeriodIncomplete),
            "clock guard remains uncertain before sixty stable seconds"
        )
        time.advance(wall: 1, continuous: 1)
        try check(
            guardUnderTest.observe().state == .ready && guardUnderTest.requestAdmission == .available,
            "clock guard admits at sixty stable seconds"
        )
        time.advance(wall: 0, continuous: 3)
        let rollback = guardUnderTest.observe()
        try check(
            rollback.state == .uncertain(.wallRollback)
                && rollback.effect == .deleteReservedRow(.wallRollback),
            "clock rollback closes admission and requires cleanup"
        )

        let server = try startLoopbackServer()
        defer { Glibc.close(server.descriptor) }
        let url = URL(string: "http://127.0.0.1:\(server.port)/probe")!
        let client = LoopbackClientOutcome()
        let completed = DispatchSemaphore(value: 0)
        let request = URLSession.shared.dataTask(with: url) { data, response, error in
            client.record(data: data, response: response, error: error)
            completed.signal()
        }
        request.resume()
        guard completed.wait(timeout: .now() + 10) == .success else {
            request.cancel()
            throw ProbeFailure(description: "FoundationNetworking loopback request timed out")
        }
        guard server.group.wait(timeout: .now() + 10) == .success else {
            throw ProbeFailure(description: "loopback server did not finish")
        }
        if let error = server.outcome.error {
            throw error
        }
        let (body, status, clientError) = client.snapshot
        if let clientError {
            throw clientError
        }
        try check(
            status == 200
                && body.map { String(decoding: $0, as: UTF8.self) } == "foundation-networking-loopback",
            "FoundationNetworking URLSession completes a real loopback HTTP request"
        )

        return checks
    }
}

do {
    let count = try UbuntuCoreProbe.run()
    guard count == 17 else {
        throw ProbeFailure(description: "probe definition drifted: expected 17 checks, ran \(count)")
    }
    print("\(count) checks passed")
} catch {
    fputs("ubuntu-core-probe failed: \(error)\n", stderr)
    Glibc.exit(1)
}
