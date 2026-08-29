import Foundation

private struct CloudCanonicalJSONTestFailure: Error, CustomStringConvertible {
    let description: String
}

/// The RFC-published vectors below were taken from the RFC 8785 text itself
/// (https://www.rfc-editor.org/rfc/rfc8785.txt): the §3.2.3 property-sorting test data, the
/// §3.2.2/§3.2.4 string-serialization sample with its exact expected bytes, and the §3.2.3
/// ""/"a"/"aa"/"ab" ordering list. The RFC's floating-point vectors are deliberately absent:
/// this unit refuses floats (see the number-scope note in Sources/CloudCanonicalJSON.swift),
/// so the "numbers" member of the §3.2.2 sample object is dropped where that sample is used.
/// The RFC's Appendix B integer rows at and beyond 2^53 are likewise absent as ACCEPTED
/// vectors: this unit's number domain stops at the ECMAScript safe integers, so those rows
/// appear below as rejection vectors instead, next to cross-runtime vectors pinning that every
/// accepted integer serializes to the same bytes ECMAScript produces.
func runCloudCanonicalJSONTests() async throws -> Int {
    var checks = 0

    func check(_ condition: @autoclosure () throws -> Bool, _ name: String) throws {
        checks += 1
        guard try condition() else {
            throw CloudCanonicalJSONTestFailure(description: "check \(checks) failed: \(name)")
        }
    }

    func checkThrows(
        _ name: String,
        _ body: () throws -> Void,
        _ matches: (CloudCanonicalJSONError) -> Bool
    ) throws {
        checks += 1
        do {
            try body()
        } catch let error as CloudCanonicalJSONError {
            guard matches(error) else {
                throw CloudCanonicalJSONTestFailure(description: "check \(checks) failed: \(name) threw unexpected \(error)")
            }
            return
        }
        throw CloudCanonicalJSONTestFailure(description: "check \(checks) failed: \(name) did not throw")
    }

    func canon(_ value: CloudJSONValue) -> String {
        String(decoding: CloudCanonicalJSON.canonicalData(value), as: UTF8.self)
    }

    func parse(_ text: String) throws -> CloudJSONValue {
        try CloudCanonicalJSON.parseStrict(Data(text.utf8))
    }

    // RFC 8785 §3.2.3: the property-sorting test data published in the RFC, with its expected
    // order — Carriage Return, One, Control, ö, €, 😀 (Emoji), דּ (Hebrew Dalet With Dagesh).
    do {
        let sortingVector: CloudJSONValue = .object([
            "\u{20AC}": .string("Euro Sign"),
            "\r": .string("Carriage Return"),
            "\u{FB33}": .string("Hebrew Letter Dalet With Dagesh"),
            "1": .string("One"),
            "\u{1F600}": .string("Emoji: Grinning Face"),
            "\u{80}": .string("Control"),
            "\u{F6}": .string("Latin Small Letter O With Diaeresis"),
        ])
        let expected = "{\"\\r\":\"Carriage Return\",\"1\":\"One\",\"\u{80}\":\"Control\",\"\u{F6}\":\"Latin Small Letter O With Diaeresis\",\"\u{20AC}\":\"Euro Sign\",\"\u{1F600}\":\"Emoji: Grinning Face\",\"\u{FB33}\":\"Hebrew Letter Dalet With Dagesh\"}"
        try check(canon(sortingVector) == expected, "RFC 8785 section 3.2.3 sorting vector serializes in the published order")
        try check(try CloudCanonicalJSON.parseStrict(CloudCanonicalJSON.canonicalData(sortingVector)) == sortingVector, "sorting vector round-trips through parseStrict")
    }

    // The UTF-16 / Unicode-scalar divergence inside that vector, pinned on its own: U+1F600 is
    // a surrogate pair (D83D DE00) whose first UTF-16 unit sorts BELOW U+FB33, while its scalar
    // value sorts ABOVE it. Scalar (or UTF-8/UTF-32) sorting flips this pair.
    do {
        let emoji = "\u{1F600}"
        let dalet = "\u{FB33}"
        try check(emoji.utf16.first! == 0xD83D && dalet.utf16.first! == 0xFB33, "surrogate fixture has the intended UTF-16 units")
        try check(emoji.unicodeScalars.first!.value > dalet.unicodeScalars.first!.value, "scalar order would put the emoji AFTER the Hebrew letter")
        let divergent: CloudJSONValue = .object([emoji: .int(1), dalet: .int(2)])
        try check(canon(divergent) == "{\"\(emoji)\":1,\"\(dalet)\":2}", "UTF-16 code unit order puts the emoji key first")
    }

    // RFC 8785 §3.2.2 string sample; expected bytes are the §3.2.4 hex dump verbatim.
    do {
        let rfcString = "\u{20AC}$\u{000F}\nA'B\"\\\\\"/"
        let rfcExpectedBytes: [UInt8] = [
            0x22, 0xE2, 0x82, 0xAC, 0x24, 0x5C, 0x75, 0x30, 0x30, 0x30, 0x66, 0x5C, 0x6E,
            0x41, 0x27, 0x42, 0x5C, 0x22, 0x5C, 0x5C, 0x5C, 0x5C, 0x5C, 0x22, 0x2F, 0x22,
        ]
        try check(CloudCanonicalJSON.canonicalData(.string(rfcString)) == Data(rfcExpectedBytes), "RFC 8785 section 3.2.4 string bytes match verbatim")

        // The §3.2.2 sample object minus its floating-point "numbers" member; the remaining
        // bytes are still the RFC's own, in the RFC's own order.
        let sample: CloudJSONValue = .object([
            "string": .string(rfcString),
            "literals": .array([.null, .bool(true), .bool(false)]),
        ])
        var expected = Data("{\"literals\":[null,true,false],\"string\":".utf8)
        expected.append(Data(rfcExpectedBytes))
        expected.append(UInt8(ascii: "}"))
        try check(CloudCanonicalJSON.canonicalData(sample) == expected, "RFC 8785 section 3.2.2 sample (numbers member dropped) matches the section 3.2.4 bytes")
        try check(canon(.array([.null, .bool(true), .bool(false)])) == "[null,true,false]", "RFC literals serialize as null/true/false")
    }

    // RFC 8785 §3.2.3 plain-English ordering list: "" < "a" < "aa" < "ab".
    do {
        let lengths: CloudJSONValue = .object(["a": .int(1), "aa": .int(2), "ab": .int(3), "": .int(0)])
        try check(canon(lengths) == "{\"\":0,\"a\":1,\"aa\":2,\"ab\":3}", "shorter key precedes longer key, empty key first")
    }

    // Cross-runtime integer vectors: the accepted domain is exactly the ECMAScript safe
    // integers, [-(2^53-1), 2^53-1]. Every expected string below is a hard-coded vector so the
    // suite stays pure Swift with zero external dependencies; each was generated on 2026-08-29
    // with node v24.1.0 via
    //   node -e 'console.log(JSON.stringify(JSON.parse("<literal>")))'
    // and for every ACCEPTED row node's output was byte-identical to the literal, so one
    // comparison pins both facts: the bytes Swift produces, and that ECMAScript produces the
    // same bytes for the same logical value.
    do {
        let acceptedVectors: [(String, Int64)] = [
            ("0", 0),
            ("1", 1),
            ("-1", -1),
            ("9007199254740991", 9_007_199_254_740_991),   // 2^53-1, Number.MAX_SAFE_INTEGER
            ("-9007199254740991", -9_007_199_254_740_991), // Number.MIN_SAFE_INTEGER
        ]
        for (expectedBytes, value) in acceptedVectors {
            try check(canon(.int(value)) == expectedBytes, "safe integer \(expectedBytes) serializes to the ECMAScript bytes")
            try check(try parse(expectedBytes) == .int(value), "safe integer \(expectedBytes) parses back to itself")
        }

        // Rejection vectors, both boundary sides. node v24.1.0's JSON.stringify(JSON.parse(s)):
        //   9007199254740993     -> 9007199254740992      (2^53+1 collapses onto 2^53)
        //   9223372036854775807  -> 9223372036854776000   (Int64.max, rewritten)
        //   -9223372036854775808 -> -9223372036854776000  (Int64.min, rewritten)
        //   9007199254740992     -> 9007199254740992      (2^53 itself round-trips today, but
        //                            the region above 2^53-1 is where integer->double stops
        //                            being injective, so the whole side fails closed)
        let rejectedLiterals = [
            "9007199254740992",     // 2^53, one past MAX_SAFE_INTEGER
            "-9007199254740992",    // -2^53, one past MIN_SAFE_INTEGER
            "9007199254740993",     // 2^53+1, the smallest literal node visibly rewrites
            "9223372036854775807",  // Int64.max
            "-9223372036854775808", // Int64.min
        ]
        for literal in rejectedLiterals {
            try checkThrows("unsafe integer \(literal) is rejected by parseStrict", { _ = try parse(literal) }) {
                $0 == .integerOutOfRange(literal)
            }
        }
        try checkThrows("unsafe integer nested in an object is rejected", { _ = try parse("{\"n\":9007199254740993}") }) {
            $0 == .integerOutOfRange("9007199254740993")
        }

        // The same domain holds outbound: a value the ECMAScript end cannot carry faithfully
        // must not become signing-input bytes either.
        try check(try CloudCanonicalJSON.signingInput(domain: "d", object: .int(9_007_199_254_740_991)) == Data("d".utf8) + Data([0x00]) + Data("9007199254740991".utf8), "MAX_SAFE_INTEGER is signable")
        for value: Int64 in [9_007_199_254_740_992, -9_007_199_254_740_992, Int64.max, Int64.min] {
            try checkThrows("unsafe integer \(value) cannot enter a signing input", { _ = try CloudCanonicalJSON.signingInput(domain: "d", object: .object(["n": .int(value)])) }) {
                $0 == .integerOutOfRange(String(value))
            }
            try checkThrows("validateNumberDomain rejects \(value) inside an array", { try CloudCanonicalJSON.validateNumberDomain(.array([.int(value)])) }) {
                $0 == .integerOutOfRange(String(value))
            }
        }
        try check({ try CloudCanonicalJSON.validateNumberDomain(.object(["hi": .int(9_007_199_254_740_991), "lo": .int(-9_007_199_254_740_991), "list": .array([.int(0)])])); return true }(), "validateNumberDomain accepts both safe extremes, nested")
    }

    // Escaping edges from §3.2.2.2: short forms, lowercase \u00xx for other C0 controls, and
    // no escaping at all for '/', DEL, or anything above U+001F.
    do {
        try check(canon(.string("\u{08}\u{09}\u{0A}\u{0C}\u{0D}")) == "\"\\b\\t\\n\\f\\r\"", "predefined controls use their short escapes")
        try check(canon(.string("\u{00}\u{1F}\u{7F}")) == "\"\\u0000\\u001f\u{7F}\"", "other controls use lowercase hex; DEL stays raw")
        try check(canon(.string("/")) == "\"/\"", "solidus is not escaped")
        try check(CloudCanonicalJSON.canonicalData(.string("\u{80}")).count == 4, "U+0080 is emitted as raw two-byte UTF-8")
    }

    // Floating-point input fails closed with the typed error naming the literal.
    do {
        for literal in ["1.5", "56.0", "2e-3", "1E30", "-0.5", "0.000001"] {
            try checkThrows("float literal \(literal) throws floatingPointUnsupported", { _ = try parse(literal) }) {
                $0 == .floatingPointUnsupported(literal)
            }
        }
        try checkThrows("integer overflow throws", { _ = try parse("9223372036854775808") }) {
            $0 == .integerOutOfRange("9223372036854775808")
        }
        try checkThrows("integer underflow throws", { _ = try parse("-9223372036854775809") }) {
            $0 == .integerOutOfRange("-9223372036854775809")
        }
    }

    // Duplicate keys are detected in raw, unescaped form — including the escaped-alias case
    // JSONSerialization would silently collapse.
    do {
        try checkThrows("duplicate key is rejected", { _ = try parse("{\"a\":1,\"a\":2}") }) {
            $0 == .duplicateKey("a")
        }
        try checkThrows("nested duplicate key is rejected", { _ = try parse("{\"x\":{\"b\":1,\"b\":2}}") }) {
            $0 == .duplicateKey("b")
        }
        try checkThrows("escaped alias of a key is still a duplicate", { _ = try parse("{\"a\":1,\"\\u0061\":2}") }) {
            $0 == .duplicateKey("a")
        }
    }

    // parseStrict accepts only bytes that are already canonical.
    do {
        try check(try parse("{\"a\":1}") == .object(["a": .int(1)]), "canonical object parses")
        try check(try parse("{}") == .object([:]), "empty object parses")
        try check(try parse("[]") == .array([]), "empty array parses")
        try check(try parse("\"\u{1F600}\"") == .string("\u{1F600}"), "raw UTF-8 emoji string parses")

        try checkThrows("interior whitespace is rejected", { _ = try parse("{\"a\": 1}") }) { $0 == .notCanonical }
        try checkThrows("leading whitespace is rejected", { _ = try parse(" {}") }) { $0 == .notCanonical }
        try checkThrows("unsorted keys are rejected", { _ = try parse("{\"b\":1,\"a\":2}") }) { $0 == .notCanonical }
        try checkThrows("needlessly escaped letter is rejected", { _ = try parse("\"\\u0041\"") }) { $0 == .notCanonical }
        try checkThrows("escaped surrogate pair is rejected as non-canonical", { _ = try parse("\"\\ud83d\\ude00\"") }) { $0 == .notCanonical }
        try checkThrows("uppercase hex escape is rejected", { _ = try parse("\"\\u001F\"") }) { $0 == .notCanonical }
        try checkThrows("minus zero re-encodes as 0 and is rejected", { _ = try parse("-0") }) { $0 == .notCanonical }

        try checkThrows("trailing garbage is rejected", { _ = try parse("{}x") }) { $0 == .trailingBytes }
        try checkThrows("trailing space is rejected", { _ = try parse("{} ") }) { $0 == .trailingBytes }
        try checkThrows("trailing newline is rejected", { _ = try parse("{}\n") }) { $0 == .trailingBytes }
        try checkThrows("number with leading zero leaves trailing bytes", { _ = try parse("01") }) { $0 == .trailingBytes }
    }

    // BOM, malformed input, lone surrogates, invalid UTF-8, nesting bound.
    do {
        var bomInput = Data([0xEF, 0xBB, 0xBF])
        bomInput.append(Data("{}".utf8))
        try checkThrows("UTF-8 BOM is rejected", { _ = try CloudCanonicalJSON.parseStrict(bomInput) }) { $0 == .byteOrderMarkRejected }

        try checkThrows("empty input is malformed", { _ = try parse("") }) {
            if case .malformedJSON = $0 { return true } else { return false }
        }
        try checkThrows("truncated literal is malformed", { _ = try parse("tru") }) {
            if case .malformedJSON = $0 { return true } else { return false }
        }
        try checkThrows("unescaped control character in string is malformed", { _ = try CloudCanonicalJSON.parseStrict(Data([0x22, 0x01, 0x22])) }) {
            if case .malformedJSON = $0 { return true } else { return false }
        }
        try checkThrows("lone high surrogate escape is rejected", { _ = try parse("\"\\ud800\"") }) { $0 == .loneSurrogate }
        try checkThrows("lone low surrogate escape is rejected", { _ = try parse("\"\\udc00\"") }) { $0 == .loneSurrogate }
        try checkThrows("high surrogate without a following escape is rejected", { _ = try parse("\"\\ud800x\"") }) { $0 == .loneSurrogate }

        try checkThrows("overlong UTF-8 is rejected", { _ = try CloudCanonicalJSON.parseStrict(Data([0x22, 0xC0, 0xAF, 0x22])) }) { $0 == .invalidUTF8 }
        try checkThrows("bad continuation byte is rejected", { _ = try CloudCanonicalJSON.parseStrict(Data([0x22, 0xE2, 0x82, 0x22])) }) { $0 == .invalidUTF8 }
        try checkThrows("UTF-8-encoded surrogate is rejected", { _ = try CloudCanonicalJSON.parseStrict(Data([0x22, 0xED, 0xA0, 0x80, 0x22])) }) { $0 == .invalidUTF8 }

        let deep = String(repeating: "[", count: 300) + String(repeating: "]", count: 300)
        try checkThrows("nesting past the sanity bound is rejected", { _ = try parse(deep) }) { $0 == .nestingTooDeep }
    }

    // Arrays keep their order; only objects sort.
    do {
        try check(canon(.array([.int(3), .int(1), .int(2)])) == "[3,1,2]", "array order is preserved")
    }

    // signingInput: domain ASCII + one 0x00 + canonical body with the TOP-LEVEL sig removed.
    do {
        let domain = "clawdline.cloud/pairing"
        let body: CloudJSONValue = .object([
            "v": .int(1),
            "sig": .string("deadbeef"),
            "payload": .object(["sig": .string("keep"), "n": .int(3)]),
        ])
        let got = try CloudCanonicalJSON.signingInput(domain: domain, object: body)
        var expected = Data(domain.utf8)
        expected.append(0x00)
        expected.append(Data("{\"payload\":{\"n\":3,\"sig\":\"keep\"},\"v\":1}".utf8))
        try check(got == expected, "signing input is domain + NUL + canonical body without top-level sig")
        try check(got.filter { $0 == 0x00 }.count == 1, "exactly one NUL separator in the signing input")
        try check(got[domain.utf8.count] == 0x00, "the NUL sits immediately after the domain")
        let jsonPart = String(decoding: got.dropFirst(domain.utf8.count + 1), as: UTF8.self)
        try check(!jsonPart.contains("deadbeef"), "top-level sig value is gone from the signing input")
        try check(jsonPart.contains("\"sig\":\"keep\""), "nested sig is preserved verbatim")

        let noSig: CloudJSONValue = .object(["v": .int(1)])
        try check(try CloudCanonicalJSON.signingInput(domain: "d", object: noSig) == Data("d".utf8) + Data([0x00]) + Data("{\"v\":1}".utf8), "body without sig is signed unchanged")
        try check(try CloudCanonicalJSON.signingInput(domain: "d", object: .array([.int(1)])) == Data("d".utf8) + Data([0x00]) + Data("[1]".utf8), "non-object body has no top-level sig to remove")

        try checkThrows("non-ASCII domain throws", { _ = try CloudCanonicalJSON.signingInput(domain: "clawdlin\u{E9}.cloud", object: noSig) }) {
            $0 == .invalidDomain("clawdlin\u{E9}.cloud")
        }
        try checkThrows("NUL inside the domain throws", { _ = try CloudCanonicalJSON.signingInput(domain: "d\u{00}d", object: noSig) }) {
            $0 == .invalidDomain("d\u{00}d")
        }
    }

    // chargedBytes counts canonical UTF-8 BYTES, never characters.
    do {
        let euroRecord: CloudJSONValue = .object(["s": .string("\u{20AC}")])
        try check(CloudCanonicalJSON.chargedBytes(record: euroRecord) == 11, "three-byte euro sign is charged as bytes")
        try check(canon(euroRecord).count == 9, "the same record is only 9 characters — bytes and characters differ")
        let emojiRecord: CloudJSONValue = .object(["e": .string("\u{1F600}")])
        try check(CloudCanonicalJSON.chargedBytes(record: emojiRecord) == 12, "four-byte emoji is charged as bytes")

        try check(CloudJSONValue.base64(Data([0xFF, 0x00, 0x10])) == .string("/wAQ"), "base64 constructor yields canonical padded standard base64")
        try check(CloudCanonicalJSON.chargedBytes(record: .object(["b": .base64(Data([0xFF, 0x00, 0x10]))])) == 12, "binary fields are charged as their base64 string form")
    }

    return checks
}
