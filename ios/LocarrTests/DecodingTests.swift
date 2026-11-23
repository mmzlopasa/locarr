import XCTest
@testable import Locarr

final class DecodingTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(T.self, from: Data(json.utf8))
    }

    func testUnknownEnumValuesAndFieldsAreKept() throws {
        let home = try decode(DiscoverHome.self, """
        {"rows":[{"key":"brand_new_row","items":[{"mediaType":"podcast","tmdbId":1,"title":"X","year":2030,
          "status":"teleporting","status4k":"none","futureField":{"a":[1,2]}}]}],
         "requests":[],"metadataReady":true,"musicReady":false,"somethingElse":42}
        """)
        XCTAssertEqual(home.rows.first?.key, "brand_new_row")
        let item = try XCTUnwrap(home.rows.first?.items.first)
        XCTAssertEqual(item.mediaType.rawValue, "podcast")
        XCTAssertEqual(item.status?.rawValue, "teleporting")
        XCTAssertNotEqual(item.status, .available)
        XCTAssertNil(TitleRef(item), "unknown media types do not open a title page")
    }

    func testRequestWithUnknownStatusAndMissingOptionalFields() throws {
        let list = try decode(RequestList.self, """
        {"items":[{"id":7,"kind":"movie","status":"on_the_moon","title":"Arrival","seasons":[],
          "explanation":{"code":"nothing_found","tone":"warning","message":"…","params":{"searches":3,"nextSearch":"2026-11-24T10:00:00Z"}}}],
         "total":1}
        """)
        let r = try XCTUnwrap(list.items.first)
        XCTAssertEqual(r.status, RequestStatus(rawValue: "on_the_moon"))
        XCTAssertNil(r.requestedBy)
        XCTAssertEqual(r.explanation?.params?["searches"].number, 3)
        XCTAssertEqual(Theme.tone(for: r.status), "neutral")
    }

    func testRoleAndPermissions() throws {
        let manager = try decode(User.self, #"{"id":2,"username":"ana","role":"manager","permissions":{"manageLibrary":true,"manageRequests":false},"language":"hr"}"#)
        XCTAssertTrue(manager.canManageLibrary)
        XCTAssertFalse(manager.canManageRequests)
        let future = try decode(User.self, #"{"id":3,"username":"b","role":"superuser"}"#)
        XCTAssertFalse(future.canManageLibrary, "an unknown role gets the least access")
        XCTAssertEqual(future.role.rawValue, "superuser")
    }

    func testTimelineEventDataOfAnyShape() throws {
        let t = try decode(Timeline.self, """
        {"item":"movie:1","explanation":{"code":"available","tone":"success","message":"ok","params":{},"reasons":[]},
         "events":[{"id":1,"at":"2026-11-23T10:00:00.123Z","type":"search","source":"search","message":"m",
                    "data":{"indexers":[{"name":"A","count":3,"status":"ok"}],"resultCount":3,"nested":{"x":null}}},
                   {"id":2,"at":"2026-11-23T10:00:00Z","type":"from_the_future","source":"timeline","message":"plain","data":null}]}
        """)
        XCTAssertEqual(t.events.count, 2)
        XCTAssertEqual(t.events[0].data?["indexers"].array.first?["name"].text, "A")
        XCTAssertEqual(Describe.event(t.events[1]), "plain", "unknown event types fall back to the server's text")
        XCTAssertNotNil(Fmt.parse(t.events[0].at))
        XCTAssertNotNil(Fmt.parse(t.events[1].at))
    }

    func testQueueAndBoardDecode() throws {
        let q = try decode(Queue.self, """
        {"items":[{"id":1,"unknown":false,"clientName":"qB","protocol":"torrent","title":"Rel","status":"active","state":"stalled_new_state",
                   "progress":0.5,"sizeBytes":100,"itemRef":"movie:3","item":{"ref":"movie:3","kind":"movie","id":3,"title":"M"}}],
         "clients":[{"id":1,"name":"qB","protocol":"torrent","enabled":true,"status":{"ok":false,"error":"refused"}}]}
        """)
        XCTAssertEqual(q.items.first?.state, "stalled_new_state")
        XCTAssertEqual(q.clients?.first?.status?.ok, false)
        let b = try decode(BoardSummary.self, #"{"total":2,"groups":[{"code":"new_code","tone":"danger","fix":"","count":2,"kinds":{"movie":2,"episode":0,"album":0,"subtitle":0},"searches":4,"reasons":[]}]}"#)
        XCTAssertEqual(Describe.boardGroupTitle(b.groups[0]), "new_code")
    }

    func testErrorBodiesBecomeAPIErrors() async {
        StubProtocol.reset { _ in .init(status: 409, body: #"{"code":"already_requested","message":"exists"}"#) }
        do {
            _ = try await stubClient().createRequest(CreateRequest(kind: "movie", tmdbId: 1))
            XCTFail("expected an error")
        } catch let e as APIError {
            XCTAssertEqual(e.code, "already_requested")
            XCTAssertEqual(e.status, 409)
            XCTAssertFalse(e.localizedText.isEmpty)
        } catch { XCTFail("\(error)") }
    }

    func testNonJSONErrorIsGeneric() async {
        StubProtocol.reset { _ in .init(status: 502, body: "<html>bad gateway</html>") }
        do {
            _ = try await stubClient().me()
            XCTFail("expected an error")
        } catch let e as APIError {
            XCTAssertEqual(e.code, "generic")
            XCTAssertEqual(e.status, 502)
        } catch { XCTFail("\(error)") }
    }

    func testClientBuildsURLsAndSendsToken() async throws {
        StubProtocol.reset { _ in .init(status: 200, body: #"{"items":[],"page":1,"totalPages":1}"#) }
        _ = try await stubClient().discoverSearch("star wars", type: "movie")
        let req = try XCTUnwrap(StubProtocol.seen.last)
        XCTAssertEqual(req.url?.path, "/api/v1/discover/search")
        XCTAssertEqual(req.query["q"], "star wars")
        XCTAssertEqual(req.value(forHTTPHeaderField: "Authorization"), "Bearer tok")
    }

    func testDownloadClientTestSendsStoredSettingsWithoutSecrets() async throws {
        StubProtocol.reset { _ in .init(status: 200, body: #"{"ok":true,"missingCategories":[],"canCreateCategories":true}"#) }
        let raw = try decode(JSONValue.self, """
        {"id":4,"name":"qB","implementation":"qbittorrent","protocol":"torrent","host":"qb","port":8080,"useTls":false,
         "password":{"configured":true},"apiKey":{"configured":false},"status":{"ok":true},"categories":{"movies":"m","series":"s","music":"a"}}
        """)
        let result = try await stubClient().testDownloadClient(raw)
        XCTAssertTrue(result.ok)
        let body = try XCTUnwrap(StubProtocol.seen.last).jsonBody
        XCTAssertEqual(body["id"] as? Int, 4)
        let client = try XCTUnwrap(body["client"] as? [String: Any])
        XCTAssertEqual(client["host"] as? String, "qb")
        XCTAssertNil(client["password"], "secrets stay on the server")
        XCTAssertNil(client["status"])
    }
}
