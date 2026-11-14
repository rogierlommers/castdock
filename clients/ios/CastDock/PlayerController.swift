@preconcurrency import AVFoundation
import MediaPlayer

struct PlayerErrorState {
    var media: String?
    var connectivity: String?

    var visibleMessage: String? { connectivity ?? media }

    mutating func clearConnectivity() {
        connectivity = nil
    }
}

@MainActor
final class PlayerController: ObservableObject {
    @Published private(set) var episode: Episode?
    @Published private(set) var isPlaying = false
    @Published private(set) var currentTime: Double = 0
    @Published private(set) var duration: Double = 0
    @Published private(set) var playbackRate: Double = 1
    @Published private(set) var playbackRateError: String?
    @Published private var errors = PlayerErrorState()

    var errorMessage: String? { errors.visibleMessage }

    static let availablePlaybackRates: [Double] = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3]
    var onPlaybackCompleted: ((Int64) -> Void)?
    var onEpisodeLoaded: ((Int64) -> Void)?
    var cachedMediaURL: ((Episode) async -> URL?)?

    private let player = AVPlayer()
    private var client: APIClient?
    private var timeObserver: Any?
    private var endObserver: NSObjectProtocol?
    private var speedUpdateTask: Task<Void, Never>?
    private var speedUpdateGeneration = 0
    private var confirmedPlaybackRate: Double = 1
    private var pendingPlaybackRate: Double?
    private var lastPeriodicSync = Date.distantPast

    init() {
        configureAudioSession()
        configureRemoteCommands()
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 1, preferredTimescale: 2), queue: .main) { [weak self] time in
            guard let self else { return }
            Task { @MainActor in
                self.currentTime = max(time.seconds, 0)
                self.duration = self.player.currentItem?.duration.seconds.isFinite == true ? self.player.currentItem!.duration.seconds : 0
                self.updateNowPlaying()
                if self.isPlaying && Date().timeIntervalSince(self.lastPeriodicSync) >= 5 {
                    self.lastPeriodicSync = Date()
                    await self.sync()
                }
            }
        }
    }

    func load(_ episode: Episode, client: APIClient, autoplay: Bool = false) async {
        speedUpdateGeneration += 1
        speedUpdateTask?.cancel()
        speedUpdateTask = nil
        pendingPlaybackRate = nil
        self.episode = episode
        onEpisodeLoaded?(episode.id)
        self.client = client
        errors = PlayerErrorState()
        playbackRateError = nil
        currentTime = episode.positionSeconds
        duration = Double(episode.durationSeconds)
        playbackRate = min(max(episode.playbackSpeed, 0.5), 3)
        confirmedPlaybackRate = playbackRate
        lastPeriodicSync = .distantPast
        let localMediaURL = await cachedMediaURL?(episode)
        guard let url = localMediaURL ?? episode.enclosureURL else {
            errors.media = "This episode does not contain a playable audio URL."
            return
        }
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        let item = AVPlayerItem(url: url)
        player.replaceCurrentItem(with: item)
        endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in await self?.finished() }
        }
        let resumeAt = CMTime(seconds: episode.positionSeconds, preferredTimescale: 600)
        await player.seek(to: resumeAt, toleranceBefore: .zero, toleranceAfter: .zero)
        updateNowPlaying()
        if autoplay { play() }
    }

    func togglePlayback() {
        isPlaying ? pause() : play()
    }

    func play() {
        guard player.currentItem != nil else { return }
        player.playImmediately(atRate: Float(playbackRate))
        isPlaying = true
        updateNowPlaying()
    }

    func pause() {
        player.pause()
        isPlaying = false
        updateNowPlaying()
        Task { await sync() }
    }

    func seek(by seconds: Double) {
        let maximum = duration > 0 ? duration : currentTime + max(seconds, 0)
        let target = min(max(currentTime + seconds, 0), maximum)
        player.seek(to: CMTime(seconds: target, preferredTimescale: 600))
        currentTime = target
        Task { await sync() }
    }

    func setPlaybackRate(_ rate: Double) {
        let rate = min(max(rate, 0.5), 3)
        guard rate != playbackRate else { return }
        playbackRate = rate
        if isPlaying { player.rate = Float(rate) }
        updateNowPlaying()

        guard let episode, let client else { return }
        playbackRateError = nil
        pendingPlaybackRate = rate
        guard speedUpdateTask == nil else { return }
        let generation = speedUpdateGeneration
        speedUpdateTask = Task { [weak self] in
            guard let self else { return }
            await persistPlaybackRates(feedID: episode.feedId, client: client, generation: generation)
        }
    }

    func stop() {
        speedUpdateGeneration += 1
        speedUpdateTask?.cancel()
        speedUpdateTask = nil
        pendingPlaybackRate = nil
        player.pause()
        player.replaceCurrentItem(with: nil)
        episode = nil
        isPlaying = false
        playbackRateError = nil
        errors = PlayerErrorState()
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }

    private func persistPlaybackRates(feedID: Int64, client: APIClient, generation: Int) async {
        defer {
            if generation == speedUpdateGeneration { speedUpdateTask = nil }
        }
        while !Task.isCancelled, generation == speedUpdateGeneration, let rate = pendingPlaybackRate {
            pendingPlaybackRate = nil
            do {
                try await client.updatePlaybackSpeed(feedID: feedID, speed: rate)
                guard !Task.isCancelled, generation == speedUpdateGeneration else { return }
                confirmedPlaybackRate = rate
                playbackRateError = nil
            } catch is CancellationError {
                return
            } catch {
                guard !Task.isCancelled, generation == speedUpdateGeneration else { return }
                guard pendingPlaybackRate == nil else { continue }
                playbackRate = confirmedPlaybackRate
                if isPlaying { player.rate = Float(confirmedPlaybackRate) }
                updateNowPlaying()
                playbackRateError = error.localizedDescription
            }
        }
    }

    func sync() async {
        await save(completed: false)
    }

    private func finished() async {
        isPlaying = false
        await save(completed: true)
    }

    private func save(completed: Bool) async {
        guard let episode, let client else { return }
        do {
            let update = PlaybackUpdate(
                position: completed ? max(duration, currentTime) : currentTime,
                duration: duration,
                completed: completed,
                playing: isPlaying,
                clientUpdatedAt: ISO8601DateFormatter().string(from: Date())
            )
            let response = try await client.savePlayback(episodeID: episode.id, update: update)
            errors.clearConnectivity()
            if completed { onPlaybackCompleted?(episode.id) }
            if completed,
               let path = response.nextEpisodeUrl,
               let nextID = Int64(path.split(separator: "/").last ?? "") {
                let next = try await client.episode(id: nextID)
                await load(next, client: client, autoplay: true)
            }
        } catch {
            errors.connectivity = error.localizedDescription
        }
    }

    func clearConnectivityError() {
        errors.clearConnectivity()
    }

    private func configureAudioSession() {
        Task.detached(priority: .userInitiated) {
            do {
                try AVAudioSession.sharedInstance().setCategory(.playback, mode: .spokenAudio)
                try AVAudioSession.sharedInstance().setActive(true)
            } catch {
                // The player surface reports media-specific failures when playback starts.
            }
        }
    }

    private func configureRemoteCommands() {
        let commands = MPRemoteCommandCenter.shared()
        commands.playCommand.addTarget { [weak self] _ in Task { @MainActor in self?.play() }; return .success }
        commands.pauseCommand.addTarget { [weak self] _ in Task { @MainActor in self?.pause() }; return .success }
        commands.skipForwardCommand.preferredIntervals = [15]
        commands.skipForwardCommand.addTarget { [weak self] _ in Task { @MainActor in self?.seek(by: 15) }; return .success }
        commands.skipBackwardCommand.preferredIntervals = [15]
        commands.skipBackwardCommand.addTarget { [weak self] _ in Task { @MainActor in self?.seek(by: -15) }; return .success }
    }

    private func updateNowPlaying() {
        guard let episode else { return }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = [
            MPMediaItemPropertyTitle: episode.title,
            MPMediaItemPropertyAlbumTitle: episode.podcastTitle,
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: currentTime,
            MPNowPlayingInfoPropertyPlaybackRate: isPlaying ? Float(playbackRate) : 0
        ]
    }
}
