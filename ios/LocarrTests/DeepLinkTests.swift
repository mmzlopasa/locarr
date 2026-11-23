import XCTest
@testable import Locarr

final class DeepLinkTests: XCTestCase {
    func testRoundTrip() throws {
        let links: [DeepLink] = [.notifications, .requests, .board, .health, .queue, .request(12), .issue(3), .movie(1), .series(2), .artist(4), .album(5)]
        for link in links {
            let parsed = try XCTUnwrap(DeepLink.parse(link.url(server: "abc")))
            XCTAssertEqual(parsed.link, link)
            XCTAssertEqual(parsed.server, "abc")
            XCTAssertNil(DeepLink.parse(link.url())?.server)
        }
    }

    func testRejectsOtherLinks() {
        XCTAssertNil(DeepLink.parse(URL(string: "https://example.com/request/1")!))
        XCTAssertNil(DeepLink.parse(URL(string: "locarr://request/abc")!))
        XCTAssertNil(DeepLink.parse(URL(string: "locarr://request")!))
        XCTAssertNil(DeepLink.parse(URL(string: "locarr://request/0")!))
        XCTAssertNil(DeepLink.parse(URL(string: "locarr://somewhere/1")!))
        XCTAssertEqual(DeepLink.parse(URL(string: "LOCARR://Request/9")!)?.link, .request(9))
    }

    func testNotificationTargets() throws {
        func n(_ type: String, request: Int64? = nil, issue: Int64? = nil) -> AppNotification {
            AppNotification(id: 1, type: type, title: "T", requestId: request, issueId: issue)
        }
        XCTAssertEqual(DeepLink.forNotification(n("request.available", request: 4)), .request(4))
        XCTAssertEqual(DeepLink.forNotification(n("issue.comment", request: 4, issue: 9)), .issue(9))
        XCTAssertEqual(DeepLink.forNotification(n("something.new")), .notifications)
    }

    func testLibraryRefs() {
        XCTAssertEqual(DeepLink.forItemRef("movie:12"), .movie(12))
        XCTAssertEqual(DeepLink.forItemRef("album:3"), .album(3))
        XCTAssertEqual(DeepLink.forItemRef("episode:9", parent: "series:4"), .series(4))
        XCTAssertNil(DeepLink.forItemRef("episode:9"))
        XCTAssertNil(DeepLink.forItemRef("garbage"))
    }

    func testRoutesAndTabs() {
        XCTAssertEqual(Route(.request(1)), .request(1))
        XCTAssertEqual(Route(.notifications), .section(.inbox))
        XCTAssertEqual(AppSection.tab(for: .request(1), manager: false), .requests)
        XCTAssertEqual(AppSection.tab(for: .section(.inbox), manager: false), .inbox)
        XCTAssertEqual(AppSection.tab(for: .section(.board), manager: true), .activity)
        XCTAssertEqual(AppSection.tab(for: .movie(1), manager: true), .library)
        XCTAssertTrue(AppSection.tabs(manager: false).allSatisfy { [.discover, .search, .requests, .inbox, .settings].contains($0) })
    }
}
