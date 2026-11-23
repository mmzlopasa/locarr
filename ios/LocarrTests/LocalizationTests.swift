import XCTest
@testable import Locarr

final class LocalizationTests: XCTestCase {
    override func tearDown() {
        Loc.override = nil
        super.tearDown()
    }

    func testPlaceholdersAndPlurals() {
        XCTAssertEqual(Loc.fill("{{a}} and {{b}}", ["a": "x", "b": "y"]), "x and y")
        XCTAssertEqual(Loc.pluralCategory(1, language: "hr"), "one")
        XCTAssertEqual(Loc.pluralCategory(21, language: "hr"), "one")
        XCTAssertEqual(Loc.pluralCategory(3, language: "hr"), "few")
        XCTAssertEqual(Loc.pluralCategory(13, language: "hr"), "other")
        XCTAssertEqual(Loc.pluralCategory(1, language: "en"), "one")
        XCTAssertEqual(Loc.pluralCategory(2, language: "en"), "other")
    }

    func testCatalogHasBothLanguages() {
        Loc.override = "en"
        XCTAssertEqual(L("library.search"), "Search")
        Loc.override = "hr"
        XCTAssertEqual(L("library.search"), "Traži")
        XCTAssertEqual(L("errors.invalid_credentials").isEmpty, false)
        XCTAssertNotEqual(L("errors.invalid_credentials"), "errors.invalid_credentials")
    }

    func testUnknownCodesFallBackToServerText() {
        Loc.override = "en"
        let ex = Explanation(code: "brand_new_code", tone: "info", message: "Server says hi", params: nil, reasons: nil)
        XCTAssertEqual(Describe.explanation(ex), "Server says hi")
        let known = Explanation(code: "downloading", tone: "info", message: "x",
                                params: .object(["title": .string("Dune"), "progress": .double(0.42), "state": .string("downloading")]), reasons: nil)
        let text = Describe.explanation(known)
        XCTAssertTrue(text.contains("Dune") && text.contains("42"), text)
        XCTAssertFalse(text.contains("{{"), text)
    }

    func testErrorCodesAreTranslated() {
        Loc.override = "hr"
        let e = APIError(status: 401, code: "invalid_credentials", message: "bad")
        XCTAssertNotEqual(e.localizedText, "bad")
        let unknown = APIError(status: 418, code: "teapot", message: "short and stout")
        XCTAssertTrue(unknown.localizedText.contains("418"))
        XCTAssertTrue(unknown.localizedText.contains("short and stout"))
    }
}
