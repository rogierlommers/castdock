import SwiftUI

struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    var body: some View {
        NavigationStack {
            List {
                if let error = model.errorMessage {
                    Section { Text(error).foregroundStyle(.red) }
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
}
