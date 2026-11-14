import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published var configuration: ServerConfiguration?
    @Published var inbox: [Episode] = []
    @Published var upNext: [Episode] = []
    @Published var isLoading = false
    @Published var errorMessage: String?
    @Published private(set) var cachedArtworkURLs: [Int64: URL] = [:]
    @Published private(set) var cachedAudioURLs: [Int64: URL] = [:]
    @Published private(set) var cachedFiles: [CachedMediaFile] = []

    let player = PlayerController()

    private let keychain = KeychainStore()
    private let mediaCache = EpisodeMediaCache()
    private(set) var apiClient: APIClient?
    private var mediaCacheTask: Task<Void, Never>?
    private var completeUpNext: [Episode] = []

    init() {
        configuration = keychain.load()
        if let configuration { apiClient = APIClient(configuration: configuration) }
        player.cachedMediaURL = { [weak self] episode in
            guard let self else { return nil }
            let cachedURL = await self.mediaCache.cachedAudioURL(for: episode)
            self.cachedAudioURLs[episode.id] = cachedURL
            return cachedURL
        }
        player.onEpisodeLoaded = { [weak self] episodeID in
            guard let self else { return }
            self.upNext = self.completeUpNext.filter { $0.id != episodeID }
        }
        player.onPlaybackCompleted = { [weak self] episodeID in
            guard let self else { return }
            self.completeUpNext.removeAll { $0.id == episodeID }
            self.upNext.removeAll { $0.id == episodeID }
            self.cachedArtworkURLs[episodeID] = nil
            self.cachedAudioURLs[episodeID] = nil
            self.cachedFiles.removeAll { $0.episodeID == episodeID }
            self.mediaCacheTask?.cancel()
            Task { [weak self] in
                guard let self else { return }
                await self.mediaCache.removeEpisode(episodeID)
                await self.refresh()
            }
        }
    }

    func connect(serverURL: String, username: String, password: String) async -> Bool {
        isLoading = true
        errorMessage = nil
        defer { isLoading = false }
        do {
            let configuration = try ServerConfiguration.make(url: serverURL, username: username, password: password)
            let client = APIClient(configuration: configuration)
            let home = try await client.connect()
            try keychain.save(configuration)
            self.configuration = configuration
            apiClient = client
            apply(home)
            return true
        } catch {
            errorMessage = error.localizedDescription
            return false
        }
    }

    func refresh() async {
        _ = await refreshFromServer()
    }

    func syncNow() async -> Bool {
        await refreshFromServer()
    }

    private func refreshFromServer() async -> Bool {
        guard let apiClient else { return false }
        isLoading = true
        defer { isLoading = false }
        do {
            apply(try await apiClient.home())
            errorMessage = nil
            player.clearConnectivityError()
            return true
        } catch APIError.unauthorized {
            do {
                try await apiClient.login()
                apply(try await apiClient.home())
                errorMessage = nil
                player.clearConnectivityError()
                return true
            } catch {
                errorMessage = error.localizedDescription
                await disconnect()
                return false
            }
        } catch {
            errorMessage = error.localizedDescription
            return false
        }
    }

    func queue(_ episode: Episode) async { await mutate { try await $0.queue(id: episode.id) } }

    func artworkURL(for episode: Episode) -> URL? {
        if let cachedURL = cachedArtworkURLs[episode.id],
           FileManager.default.fileExists(atPath: cachedURL.path) {
            return cachedURL
        }
        return episode.imageURL
    }

    func title(forCachedEpisodeID episodeID: Int64) -> String {
        if let playingEpisode = player.episode, playingEpisode.id == episodeID { return playingEpisode.title }
        return (inbox + upNext).first(where: { $0.id == episodeID })?.title ?? "Episode \(episodeID)"
    }

    func refreshCachedFiles() async {
        cachedFiles = await mediaCache.files()
    }

    func archive(_ episode: Episode) async {
        await mutate(removingCachedMediaFor: episode.id) { try await $0.archive(id: episode.id) }
    }
    func markPlayed(_ episode: Episode) async {
        await mutate(removingCachedMediaFor: episode.id) { try await $0.markPlayed(id: episode.id) }
    }

    func queueAtTop(_ episode: Episode) async {
        guard let apiClient else { return }
        do {
            try await apiClient.queue(id: episode.id)
            try await apiClient.reorder(id: episode.id, position: 1)
        } catch {
            errorMessage = error.localizedDescription
        }
        await refresh()
    }

    func playNow(_ episode: Episode) async {
        if player.episode?.id == episode.id {
            player.play()
            return
        }
        guard let apiClient else { return }
        await player.sync()
        do {
            await player.load(try await apiClient.episode(id: episode.id), client: apiClient, autoplay: true)
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func moveQueue(from offsets: IndexSet, to destination: Int) {
        guard let source = offsets.first, source != destination, source < upNext.count else { return }
        let episode = upNext[source]
        upNext.move(fromOffsets: offsets, toOffset: destination)
        let currentEpisodeID = player.episode?.id
        var reorderedEpisodes = upNext.makeIterator()
        for index in completeUpNext.indices where completeUpNext[index].id != currentEpisodeID {
            if let reorderedEpisode = reorderedEpisodes.next() {
                completeUpNext[index] = reorderedEpisode
            }
        }
        let target = (completeUpNext.firstIndex(where: { $0.id == episode.id }) ?? source) + 1
        Task {
            do {
                try await apiClient?.reorder(id: episode.id, position: target)
            } catch {
                errorMessage = error.localizedDescription
            }
            await refresh()
        }
    }

    func disconnect() async {
        player.stop()
        mediaCacheTask?.cancel()
        mediaCacheTask = nil
        await mediaCache.clear()
        keychain.clear()
        HTTPCookieStorage.shared.cookies?.forEach(HTTPCookieStorage.shared.deleteCookie)
        configuration = nil
        apiClient = nil
        inbox = []
        upNext = []
        completeUpNext = []
        cachedArtworkURLs = [:]
        cachedAudioURLs = [:]
        cachedFiles = []
    }

    private func mutate(
        removingCachedMediaFor episodeID: Int64? = nil,
        _ operation: (APIClient) async throws -> Void
    ) async {
        guard let apiClient else { return }
        do {
            try await operation(apiClient)
            if let episodeID { await removeCachedMedia(for: episodeID) }
            await refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func apply(_ home: HomeResponse) {
        inbox = home.inbox
        completeUpNext = home.upNext
        upNext = home.upNext.filter { $0.id != player.episode?.id }
        synchronizeMediaCache(for: home)
    }

    private func synchronizeMediaCache(for home: HomeResponse) {
        mediaCacheTask?.cancel()
        let episodes = home.inbox + home.upNext
        let audioEpisodeIDs = Set(home.upNext.map(\.id))
        mediaCacheTask = Task { [weak self] in
            guard let self else { return }
            await self.mediaCache.removeInactive(keeping: Set(episodes.map(\.id)))
            let existingSnapshot = await self.mediaCache.snapshot(for: episodes)
            guard !Task.isCancelled else { return }
            self.cachedArtworkURLs = existingSnapshot.artworkURLs
            self.cachedAudioURLs = existingSnapshot.audioURLs

            let snapshot = await self.mediaCache.synchronize(
                episodes: episodes,
                audioEpisodeIDs: audioEpisodeIDs
            )
            guard !Task.isCancelled else { return }
            self.cachedArtworkURLs = snapshot.artworkURLs
            self.cachedAudioURLs = snapshot.audioURLs
            self.cachedFiles = await self.mediaCache.files()
        }
    }

    private func removeCachedMedia(for episodeID: Int64) async {
        mediaCacheTask?.cancel()
        cachedArtworkURLs[episodeID] = nil
        cachedAudioURLs[episodeID] = nil
        await mediaCache.removeEpisode(episodeID)
        cachedFiles = await mediaCache.files()
    }
}
