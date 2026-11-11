import Foundation

enum APIError: LocalizedError {
    case invalidServerURL
    case invalidCredentials
    case unauthorized
    case invalidResponse
    case server(Int)
    case keychain

    var errorDescription: String? {
        switch self {
        case .invalidServerURL: "Enter a valid HTTP or HTTPS CastDock URL."
        case .invalidCredentials: "The username or password is incorrect."
        case .unauthorized: "CastDock requires authentication."
        case .invalidResponse: "CastDock returned an invalid response."
        case .server(let status): "CastDock returned HTTP \(status)."
        case .keychain: "The connection could not be stored securely."
        }
    }
}

actor APIClient {
    private let configuration: ServerConfiguration
    private let session: URLSession
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init(configuration: ServerConfiguration) {
        self.configuration = configuration
        let sessionConfiguration = URLSessionConfiguration.default
        sessionConfiguration.httpCookieStorage = .shared
        sessionConfiguration.httpShouldSetCookies = true
        sessionConfiguration.timeoutIntervalForRequest = 30
        self.session = URLSession(configuration: sessionConfiguration)
        encoder.dateEncodingStrategy = .iso8601
    }

    func connect() async throws -> HomeResponse {
        try await checkHealth()
        try await login()
        return try await home()
    }

    func checkHealth() async throws {
        let (_, response) = try await request(path: "/healthz")
        guard response.statusCode == 200 else { throw APIError.server(response.statusCode) }
    }

    func login() async throws {
        let payload = ["username": configuration.username, "password": configuration.password]
        let body = try encoder.encode(payload)
        let (_, response) = try await request(path: "/api/session", method: "POST", body: body)
        if response.statusCode == 401 { throw APIError.invalidCredentials }
        guard response.statusCode == 204 else { throw APIError.server(response.statusCode) }
    }

    func home() async throws -> HomeResponse {
        let (data, response) = try await request(path: "/api/home")
        if response.statusCode == 401 { throw APIError.unauthorized }
        guard response.statusCode == 200 else { throw APIError.server(response.statusCode) }
        return try decoder.decode(HomeResponse.self, from: data)
    }

    func episode(id: Int64) async throws -> Episode {
        let (data, response) = try await request(path: "/api/episodes/\(id)")
        guard response.statusCode == 200 else { throw APIError.server(response.statusCode) }
        return try decoder.decode(Episode.self, from: data)
    }

    func queue(id: Int64) async throws { try await emptyAction(path: "/api/episodes/\(id)/queue") }
    func archive(id: Int64) async throws { try await emptyAction(path: "/api/episodes/\(id)/archive") }
    func markPlayed(id: Int64) async throws { try await emptyAction(path: "/api/episodes/\(id)/played") }

    func reorder(id: Int64, position: Int) async throws {
        let body = try encoder.encode(QueueReorder(episodeId: id, targetPosition: position))
        let (_, response) = try await request(path: "/api/queue/reorder", method: "POST", body: body)
        guard response.statusCode == 204 else { throw APIError.server(response.statusCode) }
    }

    func savePlayback(episodeID: Int64, update: PlaybackUpdate) async throws -> PlaybackUpdateResponse {
        let body = try encoder.encode(update)
        let (data, response) = try await request(path: "/api/episodes/\(episodeID)/playback", method: "POST", body: body)
        guard response.statusCode == 200 else { throw APIError.server(response.statusCode) }
        return try decoder.decode(PlaybackUpdateResponse.self, from: data)
    }

    func updatePlaybackSpeed(feedID: Int64, speed: Double) async throws {
        let body = try encoder.encode(["playbackSpeed": speed])
        let (_, response) = try await request(path: "/api/feeds/\(feedID)/playback-options", method: "POST", body: body)
        guard response.statusCode == 204 else { throw APIError.server(response.statusCode) }
    }

    private func emptyAction(path: String) async throws {
        let (_, response) = try await request(path: path, method: "POST", body: Data())
        guard response.statusCode == 204 else { throw APIError.server(response.statusCode) }
    }

    private func request(path: String, method: String = "GET", body: Data? = nil) async throws -> (Data, HTTPURLResponse) {
        guard let url = URL(string: path, relativeTo: configuration.serverURL) else { throw APIError.invalidServerURL }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("ios", forHTTPHeaderField: "X-CastDock-Client")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, response) = try await session.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw APIError.invalidResponse }
        return (data, response)
    }
}
