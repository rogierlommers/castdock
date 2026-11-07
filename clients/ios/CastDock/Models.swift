import Foundation

struct ServerConfiguration: Codable, Equatable {
    var serverURL: URL
    var username: String
    var password: String

    static func make(url input: String, username: String, password: String) throws -> ServerConfiguration {
        var value = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if !value.contains("://") { value = "http://" + value }
        guard var components = URLComponents(string: value),
              let scheme = components.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              components.host != nil else {
            throw APIError.invalidServerURL
        }
        components.path = components.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard components.path.isEmpty, let url = components.url else { throw APIError.invalidServerURL }
        return ServerConfiguration(serverURL: url, username: username, password: password)
    }
}

struct HomeResponse: Decodable {
    let inbox: [Episode]
    let upNext: [Episode]
}

struct Episode: Decodable, Identifiable, Equatable {
    let id: Int64
    let feedId: Int64
    let podcastTitle: String
    let title: String
    let description: String
    let publishedAt: String?
    let durationSeconds: Int
    let enclosureUrl: String
    let enclosureType: String
    let imageUrl: String?
    let state: EpisodeState
    let queuePosition: Int?
    let positionSeconds: Double
    let playedDuration: Double
    let completed: Bool
    let playbackUpdatedAt: String?
    let playbackSpeed: Double

    var enclosureURL: URL? { URL(string: enclosureUrl) }
    var imageURL: URL? { imageUrl.flatMap(URL.init(string:)) }
}

enum EpisodeState: String, Decodable {
    case inbox
    case queued
    case archived
}

struct PlaybackUpdate: Encodable {
    let position: Double
    let duration: Double
    let completed: Bool
    let playing: Bool
    let clientUpdatedAt: String
}

struct PlaybackUpdateResponse: Decodable {
    let nextEpisodeUrl: String?
}

struct QueueReorder: Encodable {
    let episodeId: Int64
    let targetPosition: Int
}
