import XCTest
@testable import Locarr

final class NotificationTests: XCTestCase {
    private func item(_ id: Int64, type: String = "request.available", read: Bool = false, request: Int64? = 5) -> String {
        """
        {"id":\(id),"type":"\(type)","title":"Dune","message":"","actor":{"id":1,"username":"ana"},
         \(request.map { "\"requestId\":\($0)," } ?? "")"createdAt":"2026-11-23T10:00:00Z","read":\(read)}
        """
    }

    func testFirstPollOnlyRemembersTheNewestId() async throws {
        StubProtocol.reset { _ in .init(status: 200, body: #"{"items":[\#(self.item(41))],"unread":3,"latestId":41}"#) }
        let r = try await NotificationPoller.poll(client: stubClient(), cursor: nil)
        XCTAssertEqual(r.fresh, [])
        XCTAssertEqual(r.cursor, 41)
        XCTAssertEqual(r.unread, 3)
        XCTAssertEqual(StubProtocol.seen.last?.query["limit"], "1")
        XCTAssertNil(StubProtocol.seen.last?.query["since"])
    }

    func testLaterPollsAskSinceTheCursorAndKeepOnlyNewUnread() async throws {
        StubProtocol.reset { _ in
            .init(status: 200, body: #"{"items":[\#(self.item(44)),\#(self.item(42, read: true)),\#(self.item(43)),\#(self.item(40))],"unread":2,"latestId":44}"#)
        }
        let r = try await NotificationPoller.poll(client: stubClient(), cursor: 41)
        XCTAssertEqual(StubProtocol.seen.last?.query["since"], "41")
        XCTAssertEqual(r.fresh.map(\.id), [43, 44], "oldest first, read and old ones left out")
        XCTAssertEqual(r.cursor, 44)
    }

    func testCursorNeverGoesBack() async throws {
        StubProtocol.reset { _ in .init(status: 200, body: #"{"items":[],"unread":0,"latestId":0}"#) }
        let r = try await NotificationPoller.poll(client: stubClient(), cursor: 50)
        XCTAssertEqual(r.cursor, 50)
        XCTAssertTrue(r.fresh.isEmpty)
    }

    func testPollErrorsAreThrown() async {
        StubProtocol.reset { _ in .init(status: 401, body: #"{"code":"unauthorized","message":"x"}"#) }
        do {
            _ = try await NotificationPoller.poll(client: stubClient(), cursor: 1)
            XCTFail("expected an error")
        } catch {}
    }

    func testBannersAndCategories() throws {
        let server = SavedServer(id: "srv", url: testServer, name: "Home")
        let list = try JSONDecoder().decode(NotificationList.self, from: Data(#"{"items":[\#(item(1, type: "request.created")),\#(item(2, type: "issue.created", request: nil))],"unread":2}"#.utf8))
        let banners = NotificationPoller.banners(list.items, server: server, showServer: false)
        XCTAssertEqual(banners.count, 2)
        XCTAssertEqual(banners[0].category, LocalNotifier.requestCategory)
        XCTAssertEqual(banners[0].requestId, 5, "new requests carry the id for the Approve action")
        XCTAssertEqual(banners[0].link, DeepLink.request(5).url(server: "srv"))
        XCTAssertEqual(banners[1].category, LocalNotifier.issueCategory)
        XCTAssertNil(banners[1].requestId)
        XCTAssertEqual(banners[0].id, "srv-1", "the same notification never shows twice")
        XCTAssertFalse(banners[0].body.isEmpty)

        let many = Array(repeating: list.items[0], count: 9)
        let summary = NotificationPoller.banners(many, server: server, showServer: true)
        XCTAssertEqual(summary.count, 1)
        XCTAssertEqual(summary[0].title, "Home")
        XCTAssertEqual(summary[0].link, DeepLink.notifications.url(server: "srv"))
    }

    func testPollAllMovesEachServersCursor() async {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let store = ServerStore(defaults: defaults)
        let tokens = MemoryTokenStore()
        store.save([SavedServer(id: "a", url: testServer, name: "A", notificationCursor: 10),
                    SavedServer(id: "b", url: URL(string: "http://10.0.0.2:8484")!, name: "B"),
                    SavedServer(id: "c", url: URL(string: "http://10.0.0.3:8484")!, name: "C")])
        store.activeID = "a"
        tokens.setToken("ta", for: "a")
        tokens.setToken("tb", for: "b")
        StubProtocol.reset { req in
            req.url?.host() == "10.0.0.2"
                ? .init(status: 200, body: #"{"items":[],"unread":0,"latestId":7}"#)
                : .init(status: 200, body: #"{"items":[\#(self.item(12))],"unread":4,"latestId":12}"#)
        }
        let previous = BackgroundRefresh.enabled
        BackgroundRefresh.enabled = false
        defer { BackgroundRefresh.enabled = previous }
        let unread = await BackgroundRefresh.pollAll(store: store, tokens: tokens, session: StubProtocol.session)
        XCTAssertEqual(unread, 4)
        let saved = Dictionary(uniqueKeysWithValues: store.load().map { ($0.id, $0.notificationCursor) })
        XCTAssertEqual(saved["a"], 12)
        XCTAssertEqual(saved["b"], 7)
        XCTAssertEqual(saved["c"] ?? nil, nil, "no session, not polled")
        XCTAssertEqual(StubProtocol.seen.count, 2)
    }
}
