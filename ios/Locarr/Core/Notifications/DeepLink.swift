import Foundation

/// Screens that can be opened from outside: notification taps and `locarr://` links.
/// `locarr://request/12?server=<id>` opens request 12 on that saved server.
enum DeepLink: Hashable, Sendable {
    case notifications
    case request(Int64)
    case issue(Int64)
    case movie(Int64)
    case series(Int64)
    case artist(Int64)
    case album(Int64)
    case requests
    case board
    case health
    case queue

    static let scheme = "locarr"

    /// Parses a link; the second value is the server it belongs to, when given.
    static func parse(_ url: URL) -> (link: DeepLink, server: String?)? {
        guard url.scheme?.lowercased() == scheme, let host = url.host()?.lowercased() else { return nil }
        let parts = url.pathComponents.filter { $0 != "/" }
        let id = parts.first.flatMap { Int64($0) }
        let server = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "server" }?.value
        let link: DeepLink?
        switch host {
        case "notifications": link = .notifications
        case "requests": link = .requests
        case "board": link = .board
        case "health": link = .health
        case "queue": link = .queue
        case "request": link = id.map(DeepLink.request)
        case "issue": link = id.map(DeepLink.issue)
        case "movie": link = id.map(DeepLink.movie)
        case "series": link = id.map(DeepLink.series)
        case "artist": link = id.map(DeepLink.artist)
        case "album": link = id.map(DeepLink.album)
        default: link = nil
        }
        guard let link, id == nil || id! > 0 else { return nil }
        return (link, server?.isEmpty == true ? nil : server)
    }

    func url(server: String? = nil) -> URL {
        var comps = URLComponents()
        comps.scheme = Self.scheme
        switch self {
        case .notifications: comps.host = "notifications"
        case .requests: comps.host = "requests"
        case .board: comps.host = "board"
        case .health: comps.host = "health"
        case .queue: comps.host = "queue"
        case .request(let id): comps.host = "request"; comps.path = "/\(id)"
        case .issue(let id): comps.host = "issue"; comps.path = "/\(id)"
        case .movie(let id): comps.host = "movie"; comps.path = "/\(id)"
        case .series(let id): comps.host = "series"; comps.path = "/\(id)"
        case .artist(let id): comps.host = "artist"; comps.path = "/\(id)"
        case .album(let id): comps.host = "album"; comps.path = "/\(id)"
        }
        if let server { comps.queryItems = [URLQueryItem(name: "server", value: server)] }
        return comps.url!
    }

    /// Where a notification leads.
    static func forNotification(_ n: AppNotification) -> DeepLink {
        if let id = n.requestId, n.type.hasPrefix("request.") { return .request(id) }
        if let id = n.issueId, n.type.hasPrefix("issue.") { return .issue(id) }
        if let id = n.requestId { return .request(id) }
        if let id = n.issueId { return .issue(id) }
        return .notifications
    }

    /// The screen of a library item reference ("movie:12", "album:3", "episode:9" with its
    /// series as parent).
    static func forItemRef(_ ref: String, parent: String? = nil) -> DeepLink? {
        let parts = ref.split(separator: ":")
        guard parts.count == 2, let id = Int64(parts[1]) else { return nil }
        switch parts[0] {
        case "movie": return .movie(id)
        case "album": return .album(id)
        case "artist": return .artist(id)
        case "series": return .series(id)
        case "episode", "season":
            if let parent, parent.hasPrefix("series:") { return forItemRef(parent) }
            return nil
        default: return nil
        }
    }
}
