import XCTest
@testable import CastDock

final class CastDockTests: XCTestCase {
    func testServerConfigurationNormalizesURL() throws {
        let configuration = try ServerConfiguration.make(url: "castdock.local:8080/", username: "listener", password: "secret")
        XCTAssertEqual(configuration.serverURL.absoluteString, "http://castdock.local:8080")
        XCTAssertEqual(configuration.username, "listener")
        XCTAssertEqual(configuration.displayHostname, "castdock.local:8080")
    }

    func testServerConfigurationRejectsPath() {
        XCTAssertThrowsError(try ServerConfiguration.make(url: "https://example.com/castdock", username: "", password: ""))
    }

    func testAPIClientReauthenticatesAndRetriesAfterBackendRestart() async throws {
        let sessionConfiguration = URLSessionConfiguration.ephemeral
        sessionConfiguration.protocolClasses = [TestURLProtocol.self]
        let session = URLSession(configuration: sessionConfiguration)
        let configuration = try ServerConfiguration.make(
            url: "https://castdock.example",
            username: "listener",
            password: "secret"
        )
        let lock = NSLock()
        var requests: [String] = []
        TestURLProtocol.requestHandler = { request in
            let requestDescription = "\(request.httpMethod ?? "GET") \(request.url?.path ?? "")"
            let homeRequestCount = lock.withLock {
                requests.append(requestDescription)
                return requests.filter { $0 == "GET /api/home" }.count
            }

            let statusCode: Int
            let data: Data
            switch requestDescription {
            case "GET /api/home" where homeRequestCount == 1:
                statusCode = 401
                data = Data()
            case "POST /api/session":
                statusCode = 204
                data = Data()
            case "GET /api/home":
                statusCode = 200
                data = Data(#"{"inbox":[],"upNext":[]}"#.utf8)
            default:
                statusCode = 500
                data = Data()
            }
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: statusCode,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, data)
        }
        defer { TestURLProtocol.requestHandler = nil }

        let client = APIClient(configuration: configuration, session: session)
        let home = try await client.home()
        XCTAssertTrue(home.inbox.isEmpty)
        XCTAssertTrue(home.upNext.isEmpty)
        let completedRequests = lock.withLock { requests }
        XCTAssertEqual(completedRequests, ["GET /api/home", "POST /api/session", "GET /api/home"])
    }

    func testHomeResponseDecodesSharedAPIContract() throws {
        let json = #"{"inbox":[{"id":1,"feedId":2,"podcastTitle":"Show","title":"Episode","description":"Notes","durationSeconds":60,"enclosureUrl":"https://example.com/a.mp3","enclosureType":"audio/mpeg","state":"inbox","positionSeconds":0,"playedDuration":0,"completed":false,"playbackSpeed":1.5}],"upNext":[]}"#
        let response = try JSONDecoder().decode(HomeResponse.self, from: Data(json.utf8))
        XCTAssertEqual(response.inbox.first?.title, "Episode")
        XCTAssertEqual(response.inbox.first?.playbackSpeed, 1.5)
        XCTAssertNil(response.inbox.first?.descriptionHtml)
        XCTAssertEqual(response.inbox.first?.playTimeLeft, "1 minute")
    }

    func testHomeResponseDecodesFormattedShowNotes() throws {
        let json = #"{"inbox":[{"id":1,"feedId":2,"podcastTitle":"Show","title":"Episode","description":"First Details","descriptionHtml":"<ul><li>First</li></ul><p><a href=\"https://example.com\">Details</a></p>","durationSeconds":60,"enclosureUrl":"https://example.com/a.mp3","enclosureType":"audio/mpeg","state":"inbox","positionSeconds":0,"playedDuration":0,"completed":false,"playbackSpeed":1.5}],"upNext":[]}"#
        let response = try JSONDecoder().decode(HomeResponse.self, from: Data(json.utf8))
        XCTAssertEqual(response.inbox.first?.descriptionHtml, #"<ul><li>First</li></ul><p><a href="https://example.com">Details</a></p>"#)
    }

    func testPlayTimeLeftUsesPositionAndPlaybackSpeed() {
        XCTAssertEqual(
            PlayTimeFormatter.timeLeft(duration: 5_440, position: 610, playbackRate: 1),
            "1 hour 21 minutes"
        )
        XCTAssertEqual(
            PlayTimeFormatter.timeLeft(duration: 5_400, position: 0, playbackRate: 1.5),
            "1 hour"
        )
        XCTAssertEqual(
            PlayTimeFormatter.timeLeft(duration: 60, position: 1, playbackRate: 1),
            "1 minute"
        )
    }

    func testPlayTimeLeftOmitsUnavailableOrCompletedEpisodes() {
        XCTAssertNil(PlayTimeFormatter.timeLeft(duration: 0, position: 0, playbackRate: 1))
        XCTAssertNil(PlayTimeFormatter.timeLeft(duration: 60, position: 0, playbackRate: 0))
        XCTAssertNil(PlayTimeFormatter.timeLeft(duration: 60, position: 0, playbackRate: 1, completed: true))
    }

    func testPlayerConnectivityRecoveryClearsOnlyTheNetworkError() {
        var errors = PlayerErrorState(
            media: "The episode cannot be played.",
            connectivity: "The network connection was lost."
        )
        XCTAssertEqual(errors.visibleMessage, "The network connection was lost.")

        errors.clearConnectivity()

        XCTAssertEqual(errors.visibleMessage, "The episode cannot be played.")
        XCTAssertNil(errors.connectivity)
    }

    func testMediaCacheRemovesFilesForInactiveAndArchivedEpisodes() async throws {
        let root = FileManager.default.temporaryDirectory
            .appending(path: "CastDockMediaCacheTests-\(UUID().uuidString)", directoryHint: .isDirectory)
        defer { try? FileManager.default.removeItem(at: root) }

        let artworkDirectory = root.appending(path: "artwork", directoryHint: .isDirectory)
        let audioDirectory = root.appending(path: "audio", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: artworkDirectory, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: audioDirectory, withIntermediateDirectories: true)
        let activeArtwork = artworkDirectory.appending(path: "1")
        let archivedArtwork = artworkDirectory.appending(path: "2")
        let activeAudio = audioDirectory.appending(path: "1.mp3")
        let archivedAudio = audioDirectory.appending(path: "2.m4a")
        for file in [activeArtwork, archivedArtwork, activeAudio, archivedAudio] {
            try Data("cached".utf8).write(to: file)
        }

        let cache = EpisodeMediaCache(directory: root)
        let json = #"{"inbox":[{"id":1,"feedId":2,"podcastTitle":"Show","title":"Episode","description":"","durationSeconds":60,"enclosureUrl":"https://example.com/episode.mp3","enclosureType":"audio/mpeg","imageUrl":"https://example.com/art.jpg","state":"queued","positionSeconds":0,"playedDuration":0,"completed":false,"playbackSpeed":1}],"upNext":[]}"#
        let episode = try JSONDecoder().decode(HomeResponse.self, from: Data(json.utf8)).inbox[0]
        let cachedAudioURL = await cache.cachedAudioURL(for: episode)
        XCTAssertEqual(cachedAudioURL, activeAudio)
        let snapshot = await cache.snapshot(for: [episode])
        XCTAssertEqual(snapshot.artworkURLs[episode.id], activeArtwork)
        XCTAssertEqual(snapshot.audioURLs[episode.id], activeAudio)
        let allCachedFiles = await cache.files()
        XCTAssertEqual(allCachedFiles.count, 4)
        XCTAssertEqual(allCachedFiles.reduce(0) { $0 + $1.byteCount }, 24)

        await cache.removeInactive(keeping: [1])
        XCTAssertTrue(FileManager.default.fileExists(atPath: activeArtwork.path))
        XCTAssertTrue(FileManager.default.fileExists(atPath: activeAudio.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: archivedArtwork.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: archivedAudio.path))
        let activeCachedFiles = await cache.files()
        XCTAssertEqual(Set(activeCachedFiles.map(\.episodeID)), [1])
        XCTAssertEqual(Set(activeCachedFiles.map(\.kind)), [.artwork, .audio])

        await cache.removeEpisode(1)
        XCTAssertFalse(FileManager.default.fileExists(atPath: activeArtwork.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: activeAudio.path))
        let remainingCachedFiles = await cache.files()
        XCTAssertTrue(remainingCachedFiles.isEmpty)
    }

    func testMediaCacheUsesSafeAudioExtensions() {
        XCTAssertEqual(
            EpisodeMediaCache.audioExtension(
                sourceURL: URL(string: "https://example.com/episode")!,
                mimeType: "audio/mpeg"
            ),
            "mp3"
        )
        XCTAssertEqual(
            EpisodeMediaCache.audioExtension(
                sourceURL: URL(string: "https://example.com/episode.m4a")!,
                mimeType: nil
            ),
            "m4a"
        )
        XCTAssertEqual(
            EpisodeMediaCache.audioExtension(
                sourceURL: URL(string: "https://example.com/episode.not-safe-extension")!,
                mimeType: nil
            ),
            "media"
        )
    }
}

private final class TestURLProtocol: URLProtocol {
    nonisolated(unsafe) static var requestHandler: ((URLRequest) throws -> (HTTPURLResponse, Data))?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = Self.requestHandler else {
            client?.urlProtocol(self, didFailWithError: APIError.invalidResponse)
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
