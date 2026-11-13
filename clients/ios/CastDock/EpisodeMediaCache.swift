import Foundation
import UniformTypeIdentifiers

struct MediaCacheSnapshot: Sendable {
    let artworkURLs: [Int64: URL]
    let audioURLs: [Int64: URL]
}

enum CachedMediaKind: String, CaseIterable, Hashable, Sendable {
    case artwork
    case audio
}

struct CachedMediaFile: Identifiable, Sendable {
    let episodeID: Int64
    let kind: CachedMediaKind
    let url: URL
    let byteCount: Int64

    var id: String { url.path }
}

actor EpisodeMediaCache {
    private struct CacheRequest: Sendable {
        let episodeID: Int64
        let kind: CachedMediaKind
        let sourceURL: URL
        let mimeType: String?
    }

    private let rootDirectory: URL
    private let session: URLSession

    init(directory: URL? = nil, session: URLSession = .shared) {
        if let directory {
            rootDirectory = directory
        } else {
            let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            rootDirectory = caches.appending(path: "CastDockMedia", directoryHint: .isDirectory)
        }
        self.session = session
    }

    func synchronize(episodes: [Episode], audioEpisodeIDs: Set<Int64>) async -> MediaCacheSnapshot {
        let activeEpisodeIDs = Set(episodes.map(\.id))
        removeInactive(keeping: activeEpisodeIDs)

        var requests: [CacheRequest] = []
        for episode in episodes {
            if let imageURL = episode.imageURL {
                requests.append(CacheRequest(episodeID: episode.id, kind: .artwork, sourceURL: imageURL, mimeType: nil))
            }
            if audioEpisodeIDs.contains(episode.id), let enclosureURL = episode.enclosureURL {
                requests.append(CacheRequest(
                    episodeID: episode.id,
                    kind: .audio,
                    sourceURL: enclosureURL,
                    mimeType: episode.enclosureType
                ))
            }
        }

        return await withTaskGroup(of: (Int64, CachedMediaKind, URL)?.self) { group in
            var requestIterator = requests.makeIterator()
            for _ in 0..<min(3, requests.count) {
                guard let request = requestIterator.next() else { break }
                group.addTask { [self] in await cache(request) }
            }

            var artworkURLs: [Int64: URL] = [:]
            var audioURLs: [Int64: URL] = [:]
            while let result = await group.next() {
                if let request = requestIterator.next() {
                    group.addTask { [self] in await cache(request) }
                }
                guard !Task.isCancelled, let (episodeID, kind, url) = result else { continue }
                switch kind {
                case .artwork: artworkURLs[episodeID] = url
                case .audio: audioURLs[episodeID] = url
                }
            }
            return MediaCacheSnapshot(artworkURLs: artworkURLs, audioURLs: audioURLs)
        }
    }

    func snapshot(for episodes: [Episode]) -> MediaCacheSnapshot {
        var artworkURLs: [Int64: URL] = [:]
        var audioURLs: [Int64: URL] = [:]
        for episode in episodes {
            if episode.imageURL != nil {
                let artworkURL = directory(for: .artwork).appending(path: String(episode.id))
                if FileManager.default.fileExists(atPath: artworkURL.path) {
                    artworkURLs[episode.id] = artworkURL
                }
            }
            if let audioURL = cachedAudioURL(for: episode) {
                audioURLs[episode.id] = audioURL
            }
        }
        return MediaCacheSnapshot(artworkURLs: artworkURLs, audioURLs: audioURLs)
    }

    private func cache(_ request: CacheRequest) async -> (Int64, CachedMediaKind, URL)? {
        guard let localURL = await cacheFile(
            from: request.sourceURL,
            episodeID: request.episodeID,
            kind: request.kind,
            mimeType: request.mimeType
        ) else { return nil }
        return (request.episodeID, request.kind, localURL)
    }

    func removeEpisode(_ episodeID: Int64) {
        for kind in CachedMediaKind.allCases {
            removeFiles(in: directory(for: kind)) { cachedEpisodeID(for: $0) == episodeID }
        }
    }

    func cachedAudioURL(for episode: Episode) -> URL? {
        guard let sourceURL = episode.enclosureURL else { return nil }
        let destination = destinationURL(
            for: episode.id,
            kind: .audio,
            sourceURL: sourceURL,
            mimeType: episode.enclosureType
        )
        return FileManager.default.fileExists(atPath: destination.path) ? destination : nil
    }

    func removeInactive(keeping activeEpisodeIDs: Set<Int64>) {
        for kind in CachedMediaKind.allCases {
            removeFiles(in: directory(for: kind)) { url in
                guard let episodeID = cachedEpisodeID(for: url) else { return true }
                return !activeEpisodeIDs.contains(episodeID)
            }
        }
    }

    func clear() {
        try? FileManager.default.removeItem(at: rootDirectory)
    }

    func files() -> [CachedMediaFile] {
        var result: [CachedMediaFile] = []
        for kind in CachedMediaKind.allCases {
            let directory = directory(for: kind)
            guard let files = try? FileManager.default.contentsOfDirectory(
                at: directory,
                includingPropertiesForKeys: [.fileSizeKey],
                options: [.skipsHiddenFiles]
            ) else { continue }
            for file in files {
                guard let episodeID = cachedEpisodeID(for: file) else { continue }
                let byteCount = Int64((try? file.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
                result.append(CachedMediaFile(
                    episodeID: episodeID,
                    kind: kind,
                    url: file,
                    byteCount: byteCount
                ))
            }
        }
        return result.sorted {
            if $0.kind != $1.kind { return $0.kind.rawValue < $1.kind.rawValue }
            return $0.episodeID < $1.episodeID
        }
    }

    private func cacheFile(from sourceURL: URL, episodeID: Int64, kind: CachedMediaKind, mimeType: String?) async -> URL? {
        guard !Task.isCancelled,
              let scheme = sourceURL.scheme?.lowercased(),
              scheme == "http" || scheme == "https" else { return nil }

        let destination = destinationURL(for: episodeID, kind: kind, sourceURL: sourceURL, mimeType: mimeType)
        if FileManager.default.fileExists(atPath: destination.path) { return destination }

        do {
            try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
            let (temporaryURL, response) = try await session.download(from: sourceURL)
            guard !Task.isCancelled,
                  let response = response as? HTTPURLResponse,
                  (200...299).contains(response.statusCode) else { return nil }
            if kind == .artwork,
               let contentType = response.value(forHTTPHeaderField: "Content-Type")?.lowercased(),
               !contentType.hasPrefix("image/") {
                return nil
            }

            if FileManager.default.fileExists(atPath: destination.path) { return destination }
            try FileManager.default.moveItem(at: temporaryURL, to: destination)
            return destination
        } catch is CancellationError {
            return nil
        } catch {
            return FileManager.default.fileExists(atPath: destination.path) ? destination : nil
        }
    }

    private func destinationURL(for episodeID: Int64, kind: CachedMediaKind, sourceURL: URL, mimeType: String?) -> URL {
        let directory = directory(for: kind)
        guard kind == .audio else { return directory.appending(path: String(episodeID)) }
        let fileExtension = Self.audioExtension(sourceURL: sourceURL, mimeType: mimeType)
        return directory.appending(path: "\(episodeID).\(fileExtension)")
    }

    private func directory(for kind: CachedMediaKind) -> URL {
        rootDirectory.appending(path: kind.rawValue, directoryHint: .isDirectory)
    }

    private func removeFiles(in directory: URL, matching shouldRemove: (URL) -> Bool) {
        guard let files = try? FileManager.default.contentsOfDirectory(
            at: directory,
            includingPropertiesForKeys: nil,
            options: [.skipsHiddenFiles]
        ) else { return }
        for file in files where shouldRemove(file) {
            try? FileManager.default.removeItem(at: file)
        }
    }

    private func cachedEpisodeID(for file: URL) -> Int64? {
        Int64(file.deletingPathExtension().lastPathComponent)
    }

    static func audioExtension(sourceURL: URL, mimeType: String?) -> String {
        if let mimeType,
           let fileExtension = UTType(mimeType: mimeType)?.preferredFilenameExtension,
           isSafeFileExtension(fileExtension) {
            return fileExtension.lowercased()
        }
        let sourceExtension = sourceURL.pathExtension
        return isSafeFileExtension(sourceExtension) ? sourceExtension.lowercased() : "media"
    }

    private static func isSafeFileExtension(_ value: String) -> Bool {
        !value.isEmpty && value.count <= 8 && value.unicodeScalars.allSatisfy(CharacterSet.alphanumerics.contains)
    }
}
