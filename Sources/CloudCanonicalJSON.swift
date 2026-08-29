import Foundation

/// RFC 8785 (JSON Canonicalization Scheme) for the cloud protocol. This unit is the byte-level
/// foundation for three things: the input to Ed25519 signatures, the only legal bytes of the
/// pairing wire body, and every quota `charged_bytes` figure. One wrong byte anywhere here shows
/// up as "signature verification failed" on the other end, silently, forever.
///
/// NUMBER SCOPE — ECMAScript safe integers only, deliberately, fail closed.
/// RFC 8785 §3.2.2.3 requires numbers to be serialized with ECMAScript's `Number::toString`
/// algorithm (ECMA-262 §7.1.12.1 including its Note 2). That algorithm is genuinely hard, and
/// Swift's default `Double` formatting does NOT match it: Swift prints `1.0` where ECMAScript
/// prints `1`, and the two disagree on where scientific notation begins. An almost-right float
/// serializer produces bytes that differ between the Swift and JS ends, and that defect does not
/// fail loudly — it surfaces as signatures that never verify and `charged_bytes` that never
/// reconcile. Every number the protocol carries today is an integer (`released_rows:1`,
/// `released_bytes:270336`, `max_attempts:3`, `v:1`), so this implementation accepts integers
/// only and throws a typed error the moment any floating-point value appears, rather than doing
/// a best effort.
///
/// The integer domain is [-(2^53 - 1), 2^53 - 1] — `Number.MIN_SAFE_INTEGER` to
/// `Number.MAX_SAFE_INTEGER` — and NOT the full `Int64` range, for the same reason the floats
/// are refused, just at the other end of the number line: an ECMAScript Number is an IEEE 754
/// double, integers past 2^53 are not all exactly representable in one, and `Number::toString`
/// rewrites the ones that are not (measured with node v24.1.0: `9007199254740993` becomes
/// `9007199254740992`, and `Int64.max` becomes `9223372036854776000`). An `Int64`-ranged
/// implementation therefore produces bytes for the same logical JSON that the JS end can never
/// reproduce, and the failure is the same silent one: signatures that never verify,
/// `charged_bytes` that never reconcile. 2^53 itself happens to round-trip byte-identically
/// today, but it is the first value where integer-to-double stops being injective (2^53 + 1
/// collapses onto it), so the whole region beyond 2^53 - 1 fails closed. If a larger integer
/// domain is ever genuinely needed, first establish shared canonical number vectors that both
/// the Swift and JS implementations pass, and only then widen against those vectors — never by
/// widening the Swift side alone.
public enum CloudCanonicalJSONError: Error, Equatable {
    /// A JSON number with a fraction or exponent part. See the number-scope note above.
    case floatingPointUnsupported(String)
    /// An integer outside the ECMAScript safe-integer domain [-(2^53 - 1), 2^53 - 1]
    /// (`Number.MIN_SAFE_INTEGER` through `Number.MAX_SAFE_INTEGER`). The bound is NOT `Int64`:
    /// see the number-scope note above for why the line sits at 2^53 - 1.
    case integerOutOfRange(String)
    /// An object carried the same member name twice (compared in raw, unescaped form).
    case duplicateKey(String)
    /// Input began with a UTF-8 byte order mark.
    case byteOrderMarkRejected
    /// Bytes remained after the single top-level JSON value.
    case trailingBytes
    /// The input parsed, but its bytes are not the RFC 8785 canonical encoding of its value.
    case notCanonical
    /// The input violates the JSON grammar.
    case malformedJSON(String)
    /// The input is not well-formed UTF-8 (overlong forms and encoded surrogates included).
    case invalidUTF8
    /// A `\uXXXX` escape produced an unpaired UTF-16 surrogate (RFC 8785 §3.2.2.2 note).
    case loneSurrogate
    /// Nesting beyond `CloudCanonicalJSON.maximumNestingDepth`; a fail-closed sanity bound.
    case nestingTooDeep
    /// A signing domain containing anything but printable ASCII (0x20–0x7E). Control bytes are
    /// rejected too: a NUL inside the domain would collide with the NUL separator and let two
    /// different (domain, body) pairs share one signing input.
    case invalidDomain(String)
}

/// A JSON value the cloud protocol can canonicalize. Deliberately an enum and deliberately
/// without a float case: with `Any` the "floats must throw" rule would only surface at runtime,
/// whereas here a floating-point value is unrepresentable at the construction site. The `.int`
/// payload stays `Int64` for construction convenience, so the safe-integer bound cannot be
/// enforced here; it is enforced by `parseStrict` on every inbound byte, by `signingInput`
/// before any bytes are signed, and by `validateNumberDomain` for everything else.
public indirect enum CloudJSONValue: Equatable {
    case null
    case bool(Bool)
    case int(Int64)
    case string(String)
    case array([CloudJSONValue])
    case object([String: CloudJSONValue])

    /// The one intended way to put binary data into a record: binary fields must already be
    /// canonical standard *padded* base64 strings by the time they enter a `CloudJSONValue`
    /// tree (design §6.6), and this constructor produces exactly that form.
    public static func base64(_ data: Data) -> CloudJSONValue {
        .string(data.base64EncodedString())
    }
}

public enum CloudCanonicalJSON {
    /// Fail-closed sanity bound on container nesting (RFC 8785 §5 asks for input sanity checks).
    /// Protocol bodies are a handful of levels deep; hitting this is an attack or a bug.
    public static let maximumNestingDepth = 256

    /// ECMAScript `Number.MAX_SAFE_INTEGER` (2^53 - 1): the largest integer this unit accepts.
    public static let maximumSafeInteger: Int64 = 9_007_199_254_740_991

    /// ECMAScript `Number.MIN_SAFE_INTEGER` (-(2^53 - 1)): the smallest integer this unit accepts.
    public static let minimumSafeInteger: Int64 = -9_007_199_254_740_991

    /// Walks a value and throws `integerOutOfRange` for any integer outside the safe-integer
    /// domain. `parseStrict` enforces the domain on every inbound byte and `signingInput` calls
    /// this before serializing; a caller that produces canonical bytes through the non-throwing
    /// `canonicalData`/`chargedBytes` primitives must call this first.
    public static func validateNumberDomain(_ value: CloudJSONValue) throws {
        switch value {
        case .null, .bool, .string:
            return
        case .int(let integer):
            guard integer >= minimumSafeInteger, integer <= maximumSafeInteger else {
                throw CloudCanonicalJSONError.integerOutOfRange(String(integer))
            }
        case .array(let elements):
            for element in elements { try validateNumberDomain(element) }
        case .object(let members):
            for member in members.values { try validateNumberDomain(member) }
        }
    }

    // MARK: - Canonical serialization (RFC 8785 §3.2)

    public static func canonicalData(_ value: CloudJSONValue) -> Data {
        var out = ""
        appendCanonical(value, into: &out)
        return Data(out.utf8)
    }

    private static func appendCanonical(_ value: CloudJSONValue, into out: inout String) {
        switch value {
        case .null:
            out += "null"
        case .bool(let flag):
            out += flag ? "true" : "false"
        case .int(let integer):
            out += String(integer)
        case .string(let string):
            appendEscaped(string, into: &out)
        case .array(let elements):
            out += "["
            var first = true
            for element in elements {
                if !first { out += "," }
                first = false
                appendCanonical(element, into: &out)
            }
            out += "]"
        case .object(let members):
            out += "{"
            let sortedKeys = members.keys.sorted { $0.utf16.lexicographicallyPrecedes($1.utf16) }
            var first = true
            for key in sortedKeys {
                if !first { out += "," }
                first = false
                appendEscaped(key, into: &out)
                out += ":"
                appendCanonical(members[key]!, into: &out)
            }
            out += "}"
        }
    }

    /// RFC 8785 §3.2.2.2: escape only what must be escaped. `"` and `\` get their short forms,
    /// U+0008/09/0A/0C/0D get `\b` `\t` `\n` `\f` `\r`, every other C0 control gets lowercase
    /// `\u00xx`. Nothing else is escaped — `/` stays bare and non-ASCII is emitted as raw UTF-8.
    private static func appendEscaped(_ string: String, into out: inout String) {
        let hexDigits = Array("0123456789abcdef")
        out += "\""
        for scalar in string.unicodeScalars {
            switch scalar {
            case "\u{22}": out += "\\\""
            case "\u{5C}": out += "\\\\"
            case "\u{08}": out += "\\b"
            case "\u{09}": out += "\\t"
            case "\u{0A}": out += "\\n"
            case "\u{0C}": out += "\\f"
            case "\u{0D}": out += "\\r"
            default:
                if scalar.value < 0x20 {
                    out += "\\u00"
                    out.append(hexDigits[Int(scalar.value >> 4)])
                    out.append(hexDigits[Int(scalar.value & 0xF)])
                } else {
                    out.unicodeScalars.append(scalar)
                }
            }
        }
        out += "\""
    }

    // MARK: - Strict parsing

    /// Parses one JSON value and accepts it only if the exact input bytes are its RFC 8785
    /// canonical encoding. Rejects duplicate member names itself — Foundation's
    /// `JSONSerialization` silently keeps the last duplicate, which is precisely the trap this
    /// parser exists to close — and rejects a leading BOM, trailing bytes, malformed UTF-8,
    /// lone surrogates, and (per the number-scope note above) any floating-point number or
    /// integer outside the ECMAScript safe-integer domain.
    public static func parseStrict(_ bytes: Data) throws -> CloudJSONValue {
        let input = [UInt8](bytes)
        if input.count >= 3, input[0] == 0xEF, input[1] == 0xBB, input[2] == 0xBF {
            throw CloudCanonicalJSONError.byteOrderMarkRejected
        }
        var parser = StrictParser(input: input)
        let value = try parser.parseValue()
        guard parser.pos == input.count else {
            throw CloudCanonicalJSONError.trailingBytes
        }
        guard canonicalData(value) == bytes else {
            throw CloudCanonicalJSONError.notCanonical
        }
        return value
    }

    // MARK: - Named protocol helpers

    /// Design §5.1: the signing input is the domain's ASCII bytes, a single `0x00` separator,
    /// then the RFC 8785 canonical JSON of the body with the top-level `sig` member removed.
    /// Removal is top-level ONLY — a nested `sig` is somebody's payload data and is covered by
    /// the signature like any other byte. The domain must be printable ASCII; anything else
    /// throws (see `CloudCanonicalJSONError.invalidDomain` for why control bytes are included).
    /// A non-object value has no top-level `sig` and is canonicalized as-is.
    public static func signingInput(domain: String, object: CloudJSONValue) throws -> Data {
        try validateNumberDomain(object)
        var domainBytes = [UInt8]()
        for scalar in domain.unicodeScalars {
            guard scalar.value >= 0x20, scalar.value <= 0x7E else {
                throw CloudCanonicalJSONError.invalidDomain(domain)
            }
            domainBytes.append(UInt8(scalar.value))
        }
        let stripped: CloudJSONValue
        if case .object(var members) = object {
            members.removeValue(forKey: "sig")
            stripped = .object(members)
        } else {
            stripped = object
        }
        var data = Data(domainBytes)
        data.append(0x00)
        data.append(canonicalData(stripped))
        return data
    }

    /// Design §6.6: `charged_bytes` is the byteLength of the exact logical record serialized as
    /// RFC 8785 canonical UTF-8. Binary fields must already have been converted to canonical
    /// standard padded base64 strings by the caller before entering the record — see
    /// `CloudJSONValue.base64(_:)` — this function only measures the canonical bytes.
    public static func chargedBytes(record: CloudJSONValue) -> Int {
        canonicalData(record).count
    }
}

// MARK: - Recursive-descent JSON parser (RFC 8259 grammar, safe integers only)

private struct StrictParser {
    let input: [UInt8]
    var pos = 0
    var depth = 0

    mutating func parseValue() throws -> CloudJSONValue {
        skipWhitespace()
        guard pos < input.count else {
            throw CloudCanonicalJSONError.malformedJSON("unexpected end of input")
        }
        switch input[pos] {
        case UInt8(ascii: "{"): return try parseObject()
        case UInt8(ascii: "["): return try parseArray()
        case UInt8(ascii: "\""): return .string(try parseString())
        case UInt8(ascii: "t"): try expectLiteral("true"); return .bool(true)
        case UInt8(ascii: "f"): try expectLiteral("false"); return .bool(false)
        case UInt8(ascii: "n"): try expectLiteral("null"); return .null
        case UInt8(ascii: "-"), UInt8(ascii: "0")...UInt8(ascii: "9"): return try parseNumber()
        default:
            throw CloudCanonicalJSONError.malformedJSON("unexpected byte 0x\(String(input[pos], radix: 16)) at offset \(pos)")
        }
    }

    private mutating func parseObject() throws -> CloudJSONValue {
        try enterContainer()
        defer { depth -= 1 }
        pos += 1 // consume "{"
        var members = [String: CloudJSONValue]()
        var seen = Set<String>()
        skipWhitespace()
        if pos < input.count, input[pos] == UInt8(ascii: "}") {
            pos += 1
            return .object(members)
        }
        while true {
            skipWhitespace()
            guard pos < input.count, input[pos] == UInt8(ascii: "\"") else {
                throw CloudCanonicalJSONError.malformedJSON("expected object key at offset \(pos)")
            }
            // Duplicate detection compares raw, unescaped key strings, so "a" and "a"
            // collide as I-JSON requires.
            let key = try parseString()
            guard seen.insert(key).inserted else {
                throw CloudCanonicalJSONError.duplicateKey(key)
            }
            skipWhitespace()
            guard pos < input.count, input[pos] == UInt8(ascii: ":") else {
                throw CloudCanonicalJSONError.malformedJSON("expected ':' at offset \(pos)")
            }
            pos += 1
            members[key] = try parseValue()
            skipWhitespace()
            guard pos < input.count else {
                throw CloudCanonicalJSONError.malformedJSON("unterminated object")
            }
            if input[pos] == UInt8(ascii: ",") {
                pos += 1
                continue
            }
            if input[pos] == UInt8(ascii: "}") {
                pos += 1
                return .object(members)
            }
            throw CloudCanonicalJSONError.malformedJSON("expected ',' or '}' at offset \(pos)")
        }
    }

    private mutating func parseArray() throws -> CloudJSONValue {
        try enterContainer()
        defer { depth -= 1 }
        pos += 1 // consume "["
        var elements = [CloudJSONValue]()
        skipWhitespace()
        if pos < input.count, input[pos] == UInt8(ascii: "]") {
            pos += 1
            return .array(elements)
        }
        while true {
            elements.append(try parseValue())
            skipWhitespace()
            guard pos < input.count else {
                throw CloudCanonicalJSONError.malformedJSON("unterminated array")
            }
            if input[pos] == UInt8(ascii: ",") {
                pos += 1
                continue
            }
            if input[pos] == UInt8(ascii: "]") {
                pos += 1
                return .array(elements)
            }
            throw CloudCanonicalJSONError.malformedJSON("expected ',' or ']' at offset \(pos)")
        }
    }

    private mutating func parseString() throws -> String {
        pos += 1 // consume opening quote
        var result = ""
        while true {
            guard pos < input.count else {
                throw CloudCanonicalJSONError.malformedJSON("unterminated string")
            }
            let byte = input[pos]
            if byte == UInt8(ascii: "\"") {
                pos += 1
                return result
            }
            if byte == UInt8(ascii: "\\") {
                pos += 1
                result.unicodeScalars.append(try parseEscape())
                continue
            }
            if byte < 0x20 {
                throw CloudCanonicalJSONError.malformedJSON("unescaped control character at offset \(pos)")
            }
            if byte < 0x80 {
                result.unicodeScalars.append(Unicode.Scalar(byte))
                pos += 1
                continue
            }
            result.unicodeScalars.append(try decodeUTF8Scalar())
        }
    }

    private mutating func parseEscape() throws -> Unicode.Scalar {
        guard pos < input.count else {
            throw CloudCanonicalJSONError.malformedJSON("unterminated escape")
        }
        let byte = input[pos]
        pos += 1
        switch byte {
        case UInt8(ascii: "\""): return "\u{22}"
        case UInt8(ascii: "\\"): return "\u{5C}"
        case UInt8(ascii: "/"): return "\u{2F}"
        case UInt8(ascii: "b"): return "\u{08}"
        case UInt8(ascii: "f"): return "\u{0C}"
        case UInt8(ascii: "n"): return "\u{0A}"
        case UInt8(ascii: "r"): return "\u{0D}"
        case UInt8(ascii: "t"): return "\u{09}"
        case UInt8(ascii: "u"):
            let unit = try parseHex4()
            if (0xDC00...0xDFFF).contains(unit) {
                throw CloudCanonicalJSONError.loneSurrogate
            }
            if (0xD800...0xDBFF).contains(unit) {
                guard pos + 1 < input.count,
                      input[pos] == UInt8(ascii: "\\"),
                      input[pos + 1] == UInt8(ascii: "u") else {
                    throw CloudCanonicalJSONError.loneSurrogate
                }
                pos += 2
                let low = try parseHex4()
                guard (0xDC00...0xDFFF).contains(low) else {
                    throw CloudCanonicalJSONError.loneSurrogate
                }
                let value = 0x10000 + ((UInt32(unit) - 0xD800) << 10) + (UInt32(low) - 0xDC00)
                return Unicode.Scalar(value)!
            }
            return Unicode.Scalar(UInt32(unit))!
        default:
            throw CloudCanonicalJSONError.malformedJSON("invalid escape at offset \(pos - 1)")
        }
    }

    private mutating func parseHex4() throws -> UInt16 {
        guard pos + 4 <= input.count else {
            throw CloudCanonicalJSONError.malformedJSON("truncated \\u escape")
        }
        var value: UInt16 = 0
        for _ in 0..<4 {
            let byte = input[pos]
            let digit: UInt16
            switch byte {
            case UInt8(ascii: "0")...UInt8(ascii: "9"): digit = UInt16(byte - UInt8(ascii: "0"))
            case UInt8(ascii: "a")...UInt8(ascii: "f"): digit = UInt16(byte - UInt8(ascii: "a") + 10)
            case UInt8(ascii: "A")...UInt8(ascii: "F"): digit = UInt16(byte - UInt8(ascii: "A") + 10)
            default:
                throw CloudCanonicalJSONError.malformedJSON("invalid hex digit at offset \(pos)")
            }
            value = value << 4 | digit
            pos += 1
        }
        return value
    }

    /// Strict UTF-8: rejects bad continuation bytes, truncation, overlong forms, encoded
    /// surrogates, and anything above U+10FFFF, instead of substituting U+FFFD.
    private mutating func decodeUTF8Scalar() throws -> Unicode.Scalar {
        let first = input[pos]
        let length: Int
        var value: UInt32
        let minimum: UInt32
        if first & 0xE0 == 0xC0 {
            length = 2; value = UInt32(first & 0x1F); minimum = 0x80
        } else if first & 0xF0 == 0xE0 {
            length = 3; value = UInt32(first & 0x0F); minimum = 0x800
        } else if first & 0xF8 == 0xF0 {
            length = 4; value = UInt32(first & 0x07); minimum = 0x10000
        } else {
            throw CloudCanonicalJSONError.invalidUTF8
        }
        guard pos + length <= input.count else {
            throw CloudCanonicalJSONError.invalidUTF8
        }
        for offset in 1..<length {
            let byte = input[pos + offset]
            guard byte & 0xC0 == 0x80 else {
                throw CloudCanonicalJSONError.invalidUTF8
            }
            value = value << 6 | UInt32(byte & 0x3F)
        }
        guard value >= minimum, value <= 0x10FFFF, !(0xD800...0xDFFF).contains(value),
              let scalar = Unicode.Scalar(value) else {
            throw CloudCanonicalJSONError.invalidUTF8
        }
        pos += length
        return scalar
    }

    private mutating func parseNumber() throws -> CloudJSONValue {
        let start = pos
        if pos < input.count, input[pos] == UInt8(ascii: "-") {
            pos += 1
        }
        guard pos < input.count, isDigit(input[pos]) else {
            throw CloudCanonicalJSONError.malformedJSON("expected digit at offset \(pos)")
        }
        if input[pos] == UInt8(ascii: "0") {
            pos += 1
        } else {
            while pos < input.count, isDigit(input[pos]) { pos += 1 }
        }
        // Fraction or exponent: consume the full JSON number so the error names the whole
        // literal, then refuse it. See the number-scope note at the top of this file.
        if pos < input.count,
           input[pos] == UInt8(ascii: ".") || input[pos] == UInt8(ascii: "e") || input[pos] == UInt8(ascii: "E") {
            if input[pos] == UInt8(ascii: ".") {
                pos += 1
                guard pos < input.count, isDigit(input[pos]) else {
                    throw CloudCanonicalJSONError.malformedJSON("expected digit after '.' at offset \(pos)")
                }
                while pos < input.count, isDigit(input[pos]) { pos += 1 }
            }
            if pos < input.count,
               input[pos] == UInt8(ascii: "e") || input[pos] == UInt8(ascii: "E") {
                pos += 1
                if pos < input.count,
                   input[pos] == UInt8(ascii: "+") || input[pos] == UInt8(ascii: "-") {
                    pos += 1
                }
                guard pos < input.count, isDigit(input[pos]) else {
                    throw CloudCanonicalJSONError.malformedJSON("expected exponent digit at offset \(pos)")
                }
                while pos < input.count, isDigit(input[pos]) { pos += 1 }
            }
            let literal = String(decoding: input[start..<pos], as: UTF8.self)
            throw CloudCanonicalJSONError.floatingPointUnsupported(literal)
        }
        let literal = String(decoding: input[start..<pos], as: UTF8.self)
        guard let integer = Int64(literal),
              integer >= CloudCanonicalJSON.minimumSafeInteger,
              integer <= CloudCanonicalJSON.maximumSafeInteger else {
            throw CloudCanonicalJSONError.integerOutOfRange(literal)
        }
        return .int(integer)
    }

    private mutating func expectLiteral(_ literal: String) throws {
        let bytes = [UInt8](literal.utf8)
        guard pos + bytes.count <= input.count, Array(input[pos..<pos + bytes.count]) == bytes else {
            throw CloudCanonicalJSONError.malformedJSON("invalid literal at offset \(pos)")
        }
        pos += bytes.count
    }

    private mutating func enterContainer() throws {
        depth += 1
        guard depth <= CloudCanonicalJSON.maximumNestingDepth else {
            throw CloudCanonicalJSONError.nestingTooDeep
        }
    }

    mutating func skipWhitespace() {
        while pos < input.count {
            switch input[pos] {
            case 0x20, 0x09, 0x0A, 0x0D: pos += 1
            default: return
            }
        }
    }

    private func isDigit(_ byte: UInt8) -> Bool {
        byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9")
    }
}
