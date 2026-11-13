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

    var displayHostname: String {
        guard let host = serverURL.host else { return serverURL.absoluteString }
        guard let port = serverURL.port else { return host }
        let displayHost = host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        return "\(displayHost):\(port)"
    }
}

struct HomeResponse: Decodable {
    let inbox: [Episode]
    let upNext: [Episode]
}

struct Episode: Decodable, Identifiable, Equatable, Sendable {
    let id: Int64
    let feedId: Int64
    let podcastTitle: String
    let title: String
    let description: String
    let descriptionHtml: String?
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
    var playTimeLeft: String? {
        PlayTimeFormatter.timeLeft(
            duration: Double(durationSeconds),
            position: positionSeconds,
            playbackRate: playbackSpeed,
            completed: completed
        )
    }
}

enum PlayTimeFormatter {
    static func timeLeft(duration: Double, position: Double, playbackRate: Double, completed: Bool = false) -> String? {
        guard !completed,
              duration.isFinite, duration > 0,
              position.isFinite, position >= 0,
              playbackRate.isFinite, (0.5...3).contains(playbackRate) else { return nil }

        let totalMinutes = Int(ceil(max(0, duration - position) / playbackRate / 60))
        guard totalMinutes >= 60 else {
            return "\(totalMinutes) \(totalMinutes == 1 ? "minute" : "minutes")"
        }

        let hours = totalMinutes / 60
        let minutes = totalMinutes % 60
        let hoursText = "\(hours) \(hours == 1 ? "hour" : "hours")"
        guard minutes > 0 else { return hoursText }
        return "\(hoursText) \(minutes) \(minutes == 1 ? "minute" : "minutes")"
    }
}

enum EpisodeState: String, Decodable, Sendable {
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
