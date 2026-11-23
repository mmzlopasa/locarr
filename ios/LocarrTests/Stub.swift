import Foundation
@testable import Locarr

/// Answers URLSession requests in tests without a server.
final class StubProtocol: URLProtocol {
    struct Reply {
        var status: Int
        var body: String
    }

    nonisolated(unsafe) static var handler: ((URLRequest) -> Reply)?
    nonisolated(unsafe) static var requests: [URLRequest] = []
    private static let lock = NSLock()

    static func reset(_ handler: @escaping (URLRequest) -> Reply) {
        lock.withLock {
            self.handler = handler
            requests = []
        }
    }

    static var seen: [URLRequest] { lock.withLock { requests } }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        var req = request
        // URLSession moves the body into a stream; keep it readable for assertions.
        if req.httpBody == nil, let stream = req.httpBodyStream {
            stream.open()
            var data = Data()
            let size = 4096
            let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: size)
            while stream.hasBytesAvailable {
                let n = stream.read(buffer, maxLength: size)
                if n <= 0 { break }
                data.append(buffer, count: n)
            }
            buffer.deallocate()
            stream.close()
            req.httpBody = data
        }
        let reply: Reply = Self.lock.withLock {
            Self.requests.append(req)
            return Self.handler?(req) ?? Reply(status: 404, body: #"{"code":"not_found","message":"no stub"}"#)
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    static var session: URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubProtocol.self]
        return URLSession(configuration: config)
    }
}

extension URLRequest {
    var jsonBody: [String: Any] {
        guard let data = httpBody, let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return obj
    }

    var query: [String: String] {
        let items = URLComponents(url: url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
        return Dictionary(items.map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { a, _ in a })
    }
}

let testServer = URL(string: "http://192.168.1.10:8484")!

func stubClient(token: String? = "tok", onUnauthorized: @escaping @Sendable () -> Void = {}) -> APIClient {
    APIClient(baseURL: testServer, token: token, session: StubProtocol.session, onUnauthorized: onUnauthorized)
}
