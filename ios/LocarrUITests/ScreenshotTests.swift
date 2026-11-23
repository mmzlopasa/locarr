import XCTest

/// Captures the main screens against the app's built-in stub server (`-screenshot-stub`,
/// debug builds only). CI exports the screenshots as a workflow artifact so layout changes
/// can be checked without a device.
final class ScreenshotTests: XCTestCase {
    override func setUp() {
        continueAfterFailure = true
    }

    @MainActor
    func testMainScreens() {
        let app = launch()
        let tabs = app.tabBars.firstMatch
        XCTAssertTrue(tabs.waitForExistence(timeout: 30))
        settle()
        snap("01-overview")

        tap(tabs.buttons.element(boundBy: 1))
        snap("02-library-movies")
        tap(app.buttons["Series"].firstMatch)
        snap("03-library-series")

        tap(tabs.buttons.element(boundBy: 2))
        snap("04-discover")

        tap(tabs.buttons.element(boundBy: 3))
        snap("05-activity-queue")
        for (i, name) in ["Why didn't it land?", "Wanted", "Calendar", "History"].enumerated() {
            tap(app.buttons[name].firstMatch)
            snap("0\(6 + i)-activity-\(name.lowercased().filter(\.isLetter))")
        }

        tap(tabs.buttons.element(boundBy: 4))
        snap("10-more")
        tap(app.buttons["Requests"].firstMatch)
        snap("11-requests")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        tap(app.buttons["Issues"].firstMatch)
        snap("12-issues")
    }

    /// The Activity tab with the largest accessibility text size.
    @MainActor
    func testActivityLargeText() {
        let app = launch(extra: ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL"])
        let tabs = app.tabBars.firstMatch
        XCTAssertTrue(tabs.waitForExistence(timeout: 30))
        tap(tabs.buttons.element(boundBy: 3))
        snap("20-activity-queue-large-text")
        tap(tabs.buttons.element(boundBy: 1))
        snap("21-library-large-text")
    }

    @MainActor
    private func launch(extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-screenshot-stub", "-locarr.language", "en", "-locarr.notifications.asked", "YES"] + extra
        app.launch()
        return app
    }

    @MainActor
    private func tap(_ element: XCUIElement) {
        if element.waitForExistence(timeout: 10) { element.tap() } else { XCTFail("missing \(element)") }
        settle()
    }

    private func settle() {
        RunLoop.current.run(until: Date().addingTimeInterval(1.5))
    }

    @MainActor
    private func snap(_ name: String) {
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }
}
