import SwiftUI

struct PlayerView: View {
    @EnvironmentObject private var model: AppModel
    @ObservedObject private var player: PlayerController
    let episodeID: Int64

    init(episodeID: Int64, player: PlayerController) {
        self.episodeID = episodeID
        self.player = player
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 20) {
                AsyncImage(url: player.episode?.imageURL) { image in
                    image.resizable().scaledToFit()
                } placeholder: {
                    RoundedRectangle(cornerRadius: 18).fill(.secondary.opacity(0.15)).overlay(Image(systemName: "waveform").font(.largeTitle).foregroundStyle(.orange))
                }
                .frame(maxWidth: 320, maxHeight: 320)
                .clipShape(RoundedRectangle(cornerRadius: 18))

                VStack(spacing: 6) {
                    Text(player.episode?.title ?? "Loading…").font(.title2.bold()).multilineTextAlignment(.center)
                    Text(player.episode?.podcastTitle ?? "").foregroundStyle(.secondary)
                }

                ProgressView(value: player.currentTime, total: max(player.duration, 1))
                HStack {
                    Text(time(player.currentTime))
                    Spacer()
                    Text("−" + time(max(player.duration - player.currentTime, 0)))
                }.font(.caption.monospacedDigit()).foregroundStyle(.secondary)

                HStack(spacing: 34) {
                    Button { player.seek(by: -15) } label: { Label("15", systemImage: "gobackward.15").labelStyle(.iconOnly).font(.title) }
                    Button { player.togglePlayback() } label: { Image(systemName: player.isPlaying ? "pause.circle.fill" : "play.circle.fill").font(.system(size: 64)) }
                    Button { player.seek(by: 15) } label: { Label("15", systemImage: "goforward.15").labelStyle(.iconOnly).font(.title) }
                }
                if let error = player.errorMessage { Text(error).foregroundStyle(.red).font(.callout) }
                if let description = player.episode?.description, !description.isEmpty {
                    Divider()
                    Text(description).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .padding()
        }
        .navigationTitle("Now Playing")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: episodeID) {
            guard let client = model.apiClient else { return }
            do { player.load(try await client.episode(id: episodeID), client: client) }
            catch { player.errorMessage = error.localizedDescription }
        }
        .onDisappear { Task { await player.sync(); await model.refresh() } }
    }

    private func time(_ seconds: Double) -> String {
        guard seconds.isFinite else { return "0:00" }
        let value = max(Int(seconds), 0)
        return value >= 3600 ? String(format: "%d:%02d:%02d", value / 3600, value % 3600 / 60, value % 60) : String(format: "%d:%02d", value / 60, value % 60)
    }
}
