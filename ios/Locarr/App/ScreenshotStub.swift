#if DEBUG
import Foundation

/// Debug builds only: `-screenshot-stub` starts the app signed in to a fake server that
/// answers every request with an empty result, so the UI tests in `LocarrUITests` can
/// capture the main screens on a simulator without a Locarr server.
enum ScreenshotStub {
    static var isActive: Bool { ProcessInfo.processInfo.arguments.contains("-screenshot-stub") }

    @MainActor
    static func model() -> AppModel {
        let defaults = UserDefaults(suiteName: "locarr.screenshot-stub")!
        defaults.removePersistentDomain(forName: "locarr.screenshot-stub")
        let store = ServerStore(defaults: defaults)
        let server = SavedServer(id: "stub", url: URL(string: "http://locarr.local:8484")!, name: "Locarr")
        store.save([server])
        store.activeID = server.id
        let tokens = MemoryTokenStore()
        tokens.setToken("stub", for: server.id)
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubServer.self]
        return AppModel(store: store, tokens: tokens, urlSession: URLSession(configuration: config))
    }
}

/// Answers like an empty Locarr server with one admin account.
final class StubServer: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let path = request.url?.path ?? ""
        let body: String
        if path.hasSuffix("/auth/me") {
            body = #"{"id":1,"username":"owner","role":"admin","language":"en"}"#
        } else if path.hasSuffix("/calendar") || path.hasSuffix("/rootfolders") || path.hasSuffix("/indexers")
                    || path.hasSuffix("/downloadclients") || path.hasSuffix("/qualityprofiles") || path.hasSuffix("/providers") {
            body = "[]"
        } else {
            body = #"{"items":[],"total":0,"unread":0,"latestId":0,"groups":[],"rows":[],"issues":[],"pending":0,"hasMore":false,"metadataReady":true,"musicReady":true}"#
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
#endif
