import Foundation

/// A failed API call. `code` is the server's stable error code (or a local one:
/// `network`, `decoding`, `insecure`), shown through the `errors.*` strings.
struct APIError: Error, Sendable, Equatable {
    var status: Int
    var code: String
    var message: String

    static func network(_ message: String) -> APIError { APIError(status: 0, code: "network", message: message) }

    /// The text to show: the translated code, with the server's detail when the code is
    /// not known to this build.
    var localizedText: String {
        if code == "generic" || (!Loc.has("errors.\(code)") && status > 0) {
            let base = L("errors.generic", ["status": String(status)])
            return message.isEmpty ? base : "\(base) \(message)"
        }
        return Loc.render("errors.\(code)", [:], fallback: message.isEmpty ? L("errors.unknown") : message)
    }

    var isUnauthorized: Bool { status == 401 && code != "totp_required" && code != "invalid_credentials" && code != "invalid_totp" }
}

extension Error {
    /// User-facing text for any error.
    var userMessage: String {
        if let api = self as? APIError { return api.localizedText }
        if self is CancellationError { return "" }
        return L("errors.unknown")
    }
}

/// The hand-written Locarr API client (`Authorization: Bearer <session token>`).
final class APIClient: Sendable {
    let baseURL: URL
    let token: String?
    let session: URLSession
    private let onUnauthorized: @Sendable () -> Void

    init(baseURL: URL, token: String?, session: URLSession = APIClient.defaultSession, onUnauthorized: @escaping @Sendable () -> Void = {}) {
        self.baseURL = baseURL
        self.token = token
        self.session = session
        self.onUnauthorized = onUnauthorized
    }

    static let defaultSession: URLSession = {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 30
        config.waitsForConnectivity = false
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        return URLSession(configuration: config)
    }()

    /// `/api/v1` + path, with query items (nil values are left out).
    func url(_ path: String, query: [String: String?] = [:]) -> URL {
        var comps = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)!
        var base = comps.path
        while base.hasSuffix("/") { base.removeLast() }
        comps.percentEncodedPath = base + "/api/v1" + (path.hasPrefix("/") ? path : "/" + path)
        let items = query.compactMap { k, v in v.map { URLQueryItem(name: k, value: $0) } }.sorted { $0.name < $1.name }
        comps.queryItems = items.isEmpty ? nil : items
        return comps.url!
    }

    func get<T: Decodable & Sendable>(_ path: String, query: [String: String?] = [:]) async throws -> T {
        let data = try await raw("GET", path, query: query, body: nil)
        return try decode(data)
    }

    func send<T: Decodable & Sendable>(_ method: String, _ path: String, query: [String: String?] = [:]) async throws -> T {
        try decode(try await raw(method, path, query: query, body: nil))
    }

    func send<T: Decodable & Sendable, B: Encodable & Sendable>(_ method: String, _ path: String, query: [String: String?] = [:], body: B) async throws -> T {
        try decode(try await raw(method, path, query: query, body: try JSONEncoder().encode(body)))
    }

    func sendVoid(_ method: String, _ path: String, query: [String: String?] = [:]) async throws {
        _ = try await raw(method, path, query: query, body: nil)
    }

    func sendVoid<B: Encodable & Sendable>(_ method: String, _ path: String, query: [String: String?] = [:], body: B) async throws {
        _ = try await raw(method, path, query: query, body: try JSONEncoder().encode(body))
    }

    func decode<T: Decodable>(_ data: Data) throws -> T {
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw APIError(status: 200, code: "decoding", message: String(describing: error))
        }
    }

    /// Sends a request and returns the body of a 2xx answer; anything else throws APIError.
    func raw(_ method: String, _ path: String, query: [String: String?] = [:], body: Data?) async throws -> Data {
        var req = URLRequest(url: url(path, query: query))
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body {
            req.httpBody = body
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: req)
        } catch let error as URLError {
            if error.code == .cancelled { throw CancellationError() }
            if error.code == .appTransportSecurityRequiresSecureConnection {
                throw APIError(status: 0, code: "insecure", message: error.localizedDescription)
            }
            throw APIError.network(error.localizedDescription)
        }
        guard let http = response as? HTTPURLResponse else { throw APIError.network("no HTTP response") }
        guard (200..<300).contains(http.statusCode) else {
            let body = try? JSONDecoder().decode(ErrorBody.self, from: data)
            let err = APIError(status: http.statusCode, code: body?.code ?? "generic", message: body?.message ?? "")
            if err.isUnauthorized && token != nil { onUnauthorized() }
            throw err
        }
        return data
    }
}

/// Fields of a path segment (mbid, file names) percent-encoded.
func seg(_ s: String) -> String {
    s.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/"))) ?? s
}
