import Foundation

/// Dates, sizes and durations in the reader's language. Dates stay strings in the models so
/// an unexpected format never fails decoding; they are parsed here when shown.
enum Fmt {
    nonisolated(unsafe) private static let isoFrac: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
    nonisolated(unsafe) private static let iso: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()
    private static let dayFormat = "yyyy-MM-dd"

    /// Parses RFC 3339 timestamps (with or without fractions) and plain days.
    static func parse(_ s: String?) -> Date? {
        guard let s, !s.isEmpty else { return nil }
        if let d = isoFrac.date(from: s) ?? iso.date(from: s) { return d }
        if s.count == 10 { return day(s) }
        return nil
    }

    /// "2026-11-23" as a local calendar day.
    static func day(_ s: String) -> Date? {
        let f = DateFormatter()
        f.calendar = Calendar(identifier: .gregorian)
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = .current
        f.dateFormat = dayFormat
        return f.date(from: String(s.prefix(10)))
    }

    static func dayString(_ d: Date) -> String {
        let f = DateFormatter()
        f.calendar = Calendar(identifier: .gregorian)
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = .current
        f.dateFormat = dayFormat
        return f.string(from: d)
    }

    static func date(_ s: String?) -> String {
        guard let d = parse(s) else { return s ?? "" }
        return d.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted).locale(Loc.locale))
    }

    static func dateTime(_ s: String?) -> String {
        guard let d = parse(s) else { return s ?? "" }
        return d.formatted(Date.FormatStyle(date: .abbreviated, time: .shortened).locale(Loc.locale))
    }

    static func relative(_ s: String?) -> String {
        guard let d = parse(s) else { return s ?? "" }
        return relative(d)
    }

    static func relative(_ d: Date) -> String {
        let f = RelativeDateTimeFormatter()
        f.locale = Loc.locale
        f.unitsStyle = .short
        return f.localizedString(for: d, relativeTo: Date())
    }

    static func weekday(_ d: Date) -> String {
        d.formatted(Date.FormatStyle().weekday(.wide).day().month(.wide).locale(Loc.locale))
    }

    static func bytes(_ n: Int64?) -> String {
        guard let n else { return "–" }
        let f = ByteCountFormatter()
        f.countStyle = .file
        return f.string(fromByteCount: n)
    }

    static func speed(_ n: Int64?) -> String {
        guard let n, n > 0 else { return "–" }
        return bytes(n) + "/s"
    }

    static func duration(seconds: Int64?) -> String {
        guard let s = seconds, s > 0 else { return "–" }
        let f = DateComponentsFormatter()
        f.allowedUnits = s >= 86_400 ? [.day, .hour] : s >= 3600 ? [.hour, .minute] : [.minute, .second]
        f.unitsStyle = .abbreviated
        f.maximumUnitCount = 2
        var cal = Calendar.current
        cal.locale = Loc.locale
        f.calendar = cal
        return f.string(from: TimeInterval(s)) ?? "–"
    }

    static func percent(_ fraction: Double) -> String {
        "\(Int((fraction * 100).rounded()))%"
    }
}
