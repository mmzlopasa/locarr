import XCTest
@testable import Locarr

final class AuthTests: XCTestCase {
    private let device = DeviceInfo(name: "Test", client: "Locarr iOS")
    private let user = #"{"id":1,"username":"ana","role":"requester","permissions":{}}"#

    func testPasswordThenTotpStep() async {
        StubProtocol.reset { req in
            let body = req.jsonBody
            if body["totpCode"] == nil { return .init(status: 401, body: #"{"code":"totp_required","message":"code"}"#) }
            if body["totpCode"] as? String == "123456" { return .init(status: 200, body: #"{"user":\#(self.user),"token":"t1"}"#) }
            return .init(status: 401, body: #"{"code":"invalid_totp","message":"bad"}"#)
        }
        let client = stubClient(token: nil)
        let first = await SignIn.attempt(client: client, method: .local, username: "ana", password: "pw", code: nil, device: device)
        XCTAssertEqual(first, .needsCode)
        let wrong = await SignIn.attempt(client: client, method: .local, username: "ana", password: "pw", code: "000000", device: device)
        guard case .failed(let e) = wrong else { return XCTFail("expected failure") }
        XCTAssertEqual(e.code, "invalid_totp")
        let ok = await SignIn.attempt(client: client, method: .local, username: "ana", password: "pw", code: "123 456", device: device)
        guard case .signedIn(let token, let u) = ok else { return XCTFail("expected sign-in") }
        XCTAssertEqual(token, "t1")
        XCTAssertEqual(u.username, "ana")
        XCTAssertEqual(StubProtocol.seen.last?.url?.path, "/api/v1/auth/login")
        XCTAssertNil(StubProtocol.seen.last?.value(forHTTPHeaderField: "Authorization"))
    }

    func testStreameroProviderSignIn() async {
        StubProtocol.reset { _ in .init(status: 200, body: #"{"user":\#(self.user),"token":"t2"}"#) }
        let out = await SignIn.attempt(client: stubClient(token: nil), method: .provider(id: "streamero", name: "Streamero"),
                                       username: "ana", password: "pw", code: nil, device: device)
        XCTAssertEqual(out, .signedIn(token: "t2", user: try! JSONDecoder().decode(User.self, from: Data(user.utf8))))
        XCTAssertEqual(StubProtocol.seen.last?.url?.path, "/api/v1/auth/providers/streamero/login")
    }

    func testWrongPassword() async {
        StubProtocol.reset { _ in .init(status: 401, body: #"{"code":"invalid_credentials","message":"no"}"#) }
        let out = await SignIn.attempt(client: stubClient(token: nil), method: .local, username: "a", password: "b", code: nil, device: device)
        guard case .failed(let e) = out else { return XCTFail("expected failure") }
        XCTAssertEqual(e.code, "invalid_credentials")
        XCTAssertFalse(e.isUnauthorized, "a wrong password is not an expired session")
    }

    func testOnlyPasswordProvidersAreOffered() {
        let all = [AuthProvider(id: "local", kind: "password", name: "Locarr"), AuthProvider(id: "streamero", kind: "password", name: "Streamero"),
                   AuthProvider(id: "oidc", kind: "redirect", name: "SSO")]
        XCTAssertEqual(SignIn.passwordProviders(all).map(\.id), ["streamero"])
    }

    func testUnauthorizedCallsBack() async {
        let expectation = expectation(description: "401 reported")
        StubProtocol.reset { _ in .init(status: 401, body: #"{"code":"unauthorized","message":"expired"}"#) }
        let client = stubClient(token: "old") { expectation.fulfill() }
        _ = try? await client.me()
        await fulfillment(of: [expectation], timeout: 2)
    }

    @MainActor
    func testExpiredSessionGoesBackToSignIn() async {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let store = ServerStore(defaults: defaults)
        let tokens = MemoryTokenStore()
        let server = SavedServer(id: "s1", url: testServer, name: "Home")
        store.save([server])
        store.activeID = "s1"
        tokens.setToken("tok", for: "s1")
        StubProtocol.reset { _ in .init(status: 401, body: #"{"code":"unauthorized","message":"expired"}"#) }
        let app = AppModel(store: store, tokens: tokens, urlSession: StubProtocol.session)
        guard case .ready = app.phase else { return XCTFail("a saved token opens the session") }
        app.sessionExpired(serverID: "s1")
        guard case .signIn(let s, let notice) = app.phase else { return XCTFail("expected sign-in") }
        XCTAssertEqual(s.id, "s1")
        XCTAssertNotNil(notice)
        XCTAssertNil(tokens.token(for: "s1"))
    }

    @MainActor
    func testSeveralServers() {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let app = AppModel(store: ServerStore(defaults: defaults), tokens: MemoryTokenStore(), urlSession: StubProtocol.session)
        guard case .connect = app.phase else { return XCTFail("no server yet") }
        let a = app.addServer(url: testServer, name: "Home")
        let again = app.addServer(url: testServer, name: "Dup")
        XCTAssertEqual(a.id, again.id, "the same address is not added twice")
        let b = app.addServer(url: URL(string: "https://locarr.example.com")!, name: "")
        XCTAssertEqual(b.name, "locarr.example.com")
        app.open(b)
        guard case .signIn(let s, _) = app.phase else { return XCTFail("no token: sign in") }
        XCTAssertEqual(s.id, b.id)
        app.forget(b)
        XCTAssertEqual(app.servers.map(\.id), [a.id])
        XCTAssertEqual(app.activeServer?.id, a.id)
    }

    func testServerAddresses() {
        XCTAssertEqual(ServerURL.normalize("192.168.1.10:8484")?.absoluteString, "http://192.168.1.10:8484")
        XCTAssertEqual(ServerURL.normalize("nas.local:8484/")?.absoluteString, "http://nas.local:8484")
        XCTAssertEqual(ServerURL.normalize("locarr.example.com")?.absoluteString, "https://locarr.example.com")
        XCTAssertEqual(ServerURL.normalize("http://10.0.0.5:8484/api/v1")?.absoluteString, "http://10.0.0.5:8484")
        XCTAssertEqual(ServerURL.normalize("https://example.com/locarr/")?.absoluteString, "https://example.com/locarr")
        XCTAssertNil(ServerURL.normalize("   "))
        XCTAssertNil(ServerURL.normalize("ftp://x"))
        XCTAssertTrue(ServerURL.isAllowed(URL(string: "http://172.20.1.1:8484")!))
        XCTAssertTrue(ServerURL.isAllowed(URL(string: "http://100.101.1.2")!))
        XCTAssertTrue(ServerURL.isAllowed(URL(string: "http://locarr.home.arpa")!))
        XCTAssertTrue(ServerURL.isAllowed(URL(string: "http://nas")!))
        XCTAssertFalse(ServerURL.isAllowed(URL(string: "http://172.32.0.1")!))
        XCTAssertFalse(ServerURL.isAllowed(URL(string: "http://locarr.example.com")!))
        XCTAssertTrue(ServerURL.isAllowed(URL(string: "https://locarr.example.com")!))
    }

    func testSubPathIsKeptInAPIURLs() {
        let client = APIClient(baseURL: URL(string: "https://example.com/locarr")!, token: nil)
        XCTAssertEqual(client.url("/movies", query: ["q": "a b"]).absoluteString, "https://example.com/locarr/api/v1/movies?q=a%20b")
    }
}
