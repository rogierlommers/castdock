import SwiftUI

struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    var body: some View {
        NavigationStack {
            List {
                if let error = model.errorMessage {
                    Section { Text(error).foregroundStyle(.red) }
                }
                if let episode = model.player.episode {
                    nowPlayingSection(episode)
                }
                Section("Inbox · \(model.inbox.count)") {
                    if model.inbox.isEmpty { ContentUnavailableView("Inbox zero", systemImage: "tray") }
                    ForEach(model.inbox) { episode in
                        episodeLink(episode)
                            .swipeActions(edge: .trailing) {
                                Button("Archive") { Task { await model.archive(episode) } }.tint(.gray)
                                Button("Queue") { Task { await model.queue(episode) } }.tint(.orange)
                            }
                            .swipeActions(edge: .leading) {
                                Button("Played") { Task { await model.markPlayed(episode) } }.tint(.green)
                            }
                    }
                }
                Section("Up Next · \(model.upNext.count)") {
                    if model.upNext.isEmpty { ContentUnavailableView("Nothing queued", systemImage: "text.line.first.and.arrowtriangle.forward") }
                    ForEach(model.upNext) { episode in
                        episodeLink(episode)
                            .swipeActions {
                                Button("Remove") { Task { await model.archive(episode) } }.tint(.gray)
                                Button("Played") { Task { await model.markPlayed(episode) } }.tint(.green)
                            }
                    }
                    .onMove(perform: model.moveQueue)
                }
            }
            .navigationTitle("CastDock")
            .refreshable { await model.refresh() }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { EditButton() }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button("Disconnect", role: .destructive) { model.disconnect() }
                    } label: { Image(systemName: "ellipsis.circle") }
                }
            }
            .overlay { if model.isLoading && model.inbox.isEmpty && model.upNext.isEmpty { ProgressView() } }
            .navigationDestination(for: Int64.self) { id in PlayerView(episodeID: id, player: model.player) }
            .task { await model.refresh() }
        }
    }

    private func episodeLink(_ episode: Episode) -> some View {
        NavigationLink(value: episode.id) { EpisodeRow(episode: episode) }
    }

    private func nowPlayingSection(_ episode: Episode) -> some View {
        Section("Now Playing") {
            NavigationLink(value: episode.id) {
                HStack(spacing: 12) {
                    AsyncImage(url: episode.imageURL) { image in
                        image.resizable().scaledToFill()
                    } placeholder: {
                        ZStack {
                            Color.secondary.opacity(0.15)
                            Image(systemName: "waveform").foregroundStyle(.orange)
                        }
                    }
                    .frame(width: 56, height: 56)
                    .clipShape(RoundedRectangle(cornerRadius: 9))

                    VStack(alignment: .leading, spacing: 4) {
                        Text(episode.title).font(.headline).lineLimit(2)
                        Text(episode.podcastTitle).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        ProgressView(value: model.player.currentTime, total: max(model.player.duration, 1))
                    }
                }
            }

            HStack {
                Button { model.player.seek(by: -15) } label: {
                    Label("Rewind 15 seconds", systemImage: "gobackward.15").labelStyle(.iconOnly).font(.title2)
                }
                .accessibilityLabel("Rewind 15 seconds")

                Spacer()

                Button { model.player.togglePlayback() } label: {
                    Label(model.player.isPlaying ? "Pause" : "Play", systemImage: model.player.isPlaying ? "pause.circle.fill" : "play.circle.fill")
                        .labelStyle(.iconOnly)
                        .font(.system(size: 44))
                }
                .accessibilityLabel(model.player.isPlaying ? "Pause" : "Play")

                Spacer()

                Button { model.player.seek(by: 15) } label: {
                    Label("Forward 15 seconds", systemImage: "goforward.15").labelStyle(.iconOnly).font(.title2)
                }
                .accessibilityLabel("Forward 15 seconds")
            }
            .buttonStyle(.borderless)
            .padding(.horizontal, 24)

            Picker("Playback speed", selection: Binding(
                get: { model.player.playbackRate },
                set: { model.player.setPlaybackRate($0) }
            )) {
                ForEach(PlayerController.availablePlaybackRates, id: \.self) { rate in
                    Text(rate.formatted(.number.precision(.fractionLength(0...2))) + "×").tag(rate)
                }
            }
            .pickerStyle(.menu)

            if let error = model.player.errorMessage {
                Text(error).foregroundStyle(.red).font(.callout)
            }
            if let error = model.player.playbackRateError {
                Text("Playback speed could not be saved: \(error)").foregroundStyle(.red).font(.callout)
            }
        }
    }
}
