import Foundation

@main
struct ShellLanguageTest {
    static func placeholders(_ value: String) -> Set<String> {
        let regex = try! NSRegularExpression(pattern: "\\{arg[0-9]+\\}")
        let range = NSRange(value.startIndex..<value.endIndex, in: value)
        return Set(regex.matches(in: value, range: range).compactMap {
            Range($0.range, in: value).map { String(value[$0]) }
        })
    }

    static func main() throws {
        let cases = [
            ("en-US", "en"), ("zh-Hant-CN", "zh-Hant"),
            ("zh-Hans-TW", "zh-Hans"), ("zh-TW", "zh-Hant"),
            ("zh-CN", "zh-Hans"), ("ja-JP", "ja"),
            ("ko-KR", "ko"), ("pt-PT", "pt-BR"),
            ("es-MX", "es"), ("fr-CA", "fr"), ("de-AT", "de"),
        ]
        for (input, expected) in cases {
            precondition(ShellLanguage.resolveTag(input) == expected, input)
        }
        precondition(ShellLanguage.resolveTag("zh-Latn-TW") == nil)
        precondition(ShellLanguage.resolve("auto", languages: ["zh", "ja-JP"]) == "ja")

        let data = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
        try ShellLanguage.loadCatalog(data)
        let catalog = try JSONDecoder().decode([String: [String: String]].self, from: data)
        precondition(Set(catalog.keys) == Set(ShellLanguage.tags))
        let keys = Set(catalog["en"]!.keys)
        for tag in ShellLanguage.tags {
            let translated = Set(catalog[tag]!.keys)
            precondition(translated.isSubset(of: keys), "\(tag) unknown keys")
            if tag == "en" || tag == "zh-Hant" {
                precondition(translated == keys, "\(tag) keys")
            }
            _ = ShellLanguage.select(tag)
            for key in keys {
                // Later additions may fall back to English, matching the
                // shipped two-language localization gate.
                let template = catalog[tag]![key] ?? catalog["en"]![key]!
                precondition(!template.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                let rendered = ShellLanguage.word(key, ["VALUE_0", "VALUE_1", "VALUE_2", "VALUE_3"])
                precondition(!rendered.contains("{arg"), "\(tag).\(key)")
                precondition(rendered != key)
                let references = placeholders(catalog["en"]![key]!)
                let actual = placeholders(template)
                precondition(actual == references, "\(tag).\(key) placeholders")
            }
            precondition(ShellLanguage.tag == tag)
            precondition(!ShellLanguage.select(tag))
        }
        precondition(!ShellLanguage.select("unknown"))
        _ = ShellLanguage.select("en")
        precondition(ShellLanguage.word("statusWorking", ["{arg1}"]).contains("{arg1}"),
                     "an authored label must not be interpolated twice")
        precondition(ShellLanguage.word("menuWindow", language: "ja") == catalog["ja"]!["menuWindow"])
        precondition(ShellLanguage.htmlText("<&>\"'") == "&lt;&amp;&gt;&quot;&#39;")
        print("Swift shell catalog: 9 languages, \(keys.count) keys, resolver, switching and interpolation passed")
    }
}
