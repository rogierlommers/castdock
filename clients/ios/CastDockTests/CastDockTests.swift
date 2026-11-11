import XCTest
@testable import CastDock

final class CastDockTests: XCTestCase {
    func testServerConfigurationNormalizesURL() throws {
        let configuration = try ServerConfiguration.make(url: "castdock.local:8080/", username: "listener", password: "secret")
        XCTAssertEqual(configuration.serverURL.absoluteString, "http://castdock.local:8080")
        XCTAssertEqual(configuration.username, "listener")
    }

    func testServerConfigurationRejectsPath() {
        XCTAssertThrowsError(try ServerConfiguration.make(url: "https://example.com/castdock", username: "", password: ""))
    }

    func testHomeResponseDecodesSharedAPIContract() throws {
        let json = #"{"inbox":[{"id":1,"feedId":2,"podcastTitle":"Show","title":"Episode","description":"Notes","durationSeconds":60,"enclosureUrl":"https://example.com/a.mp3","enclosureType":"audio/mpeg","state":"inbox","positionSeconds":0,"playedDuration":0,"completed":false,"playbackSpeed":1.5}],"upNext":[]}"#
        let response = try JSONDecoder().decode(HomeResponse.self, from: Data(json.utf8))
        XCTAssertEqual(response.inbox.first?.title, "Episode")
        XCTAssertEqual(response.inbox.first?.playbackSpeed, 1.5)
        XCTAssertNil(response.inbox.first?.descriptionHtml)
    }

    func testHomeResponseDecodesFormattedShowNotes() throws {
        let json = #"{"inbox":[{"id":1,"feedId":2,"podcastTitle":"Show","title":"Episode","description":"First Details","descriptionHtml":"<ul><li>First</li></ul><p><a href=\"https://example.com\">Details</a></p>","durationSeconds":60,"enclosureUrl":"https://example.com/a.mp3","enclosureType":"audio/mpeg","state":"inbox","positionSeconds":0,"playedDuration":0,"completed":false,"playbackSpeed":1.5}],"upNext":[]}"#
        let response = try JSONDecoder().decode(HomeResponse.self, from: Data(json.utf8))
        XCTAssertEqual(response.inbox.first?.descriptionHtml, #"<ul><li>First</li></ul><p><a href="https://example.com">Details</a></p>"#)
    }
}
