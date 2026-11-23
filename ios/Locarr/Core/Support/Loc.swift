import Foundation

/// Every user-visible string comes from the string catalog by key. Keys shared with the web
/// client (`errors.*`, `explain.*`, `board.*` …) are copied from its locales by
/// `scripts/ios-strings.py`, with the same `{{name}}` placeholders.
enum Loc {
    private static let key = "locarr.language"

    /// "en", "hr" or nil (follow the system).
    static var override: String? {
        get { UserDefaults.standard.string(forKey: key) }
        set {
            if let newValue { UserDefaults.standard.set(newValue, forKey: key) } else { UserDefaults.standard.removeObject(forKey: key) }
        }
    }

    /// The language strings are shown in.
    static var language: String {
        if let override { return override }
        let preferred = Bundle.main.preferredLocalizations.first ?? "en"
        return preferred.hasPrefix("hr") ? "hr" : "en"
    }

    static var locale: Locale { Locale(identifier: language == "hr" ? "hr_HR" : "en_US") }

    private static func bundle(for language: String) -> Bundle {
        if let path = Bundle.main.path(forResource: language, ofType: "lproj"), let b = Bundle(path: path) { return b }
        return Bundle.main
    }

    /// The raw catalog value, or nil when the key is missing.
    static func lookup(_ key: String) -> String? {
        let missing = "\u{1}missing"
        let value = bundle(for: language).localizedString(forKey: key, value: missing, table: nil)
        if value != missing { return value }
        let en = bundle(for: "en").localizedString(forKey: key, value: missing, table: nil)
        return en == missing ? nil : en
    }

    static func string(_ key: String) -> String { lookup(key) ?? key }

    static func has(_ key: String) -> Bool { lookup(key) != nil }

    /// Fills `{{name}}` placeholders. With a numeric `count` the plural form
    /// (`key_one`, `key_few`, `key_other`) is used when the catalog has one.
    static func render(_ key: String, _ params: [String: String] = [:], fallback: String? = nil) -> String {
        var template: String?
        if let count = params["count"].flatMap({ Int($0) }) {
            template = lookup("\(key)_\(pluralCategory(count, language: language))") ?? lookup("\(key)_other")
        }
        guard let text = template ?? lookup(key) ?? fallback else { return key }
        return fill(text, params)
    }

    static func fill(_ template: String, _ params: [String: String]) -> String {
        var out = template
        for (k, v) in params { out = out.replacingOccurrences(of: "{{\(k)}}", with: v) }
        return out
    }

    /// CLDR plural category for English and Croatian.
    static func pluralCategory(_ n: Int, language: String) -> String {
        if language == "hr" {
            let m10 = n % 10, m100 = n % 100
            if m10 == 1 && m100 != 11 { return "one" }
            if (2...4).contains(m10) && !(12...14).contains(m100) { return "few" }
            return "other"
        }
        return n == 1 ? "one" : "other"
    }
}

/// Shorthand for `Loc.string` / `Loc.render`.
func L(_ key: String) -> String { Loc.string(key) }
func L(_ key: String, _ params: [String: String]) -> String { Loc.render(key, params) }
