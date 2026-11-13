import Foundation
import SwiftUI

struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    private var audioFiles: [CachedMediaFile] {
        model.cachedFiles.filter { $0.kind == .audio }
    }

    private var artworkFiles: [CachedMediaFile] {
        model.cachedFiles.filter { $0.kind == .artwork }
    }

    private var totalByteCount: Int64 {
        model.cachedFiles.reduce(0) { $0 + $1.byteCount }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Connection") {
                    LabeledContent(
                        "Hostname",
                        value: model.configuration?.displayHostname ?? "Not connected"
                    )
                    Button("Disconnect", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                        Task {
                            dismiss()
                            await model.disconnect()
                        }
                    }
                }

                Section("Cache overview") {
                    LabeledContent("Files", value: model.cachedFiles.count.formatted())
                    LabeledContent("Storage used", value: formatBytes(totalByteCount))
                    Text("Artwork is cached for active episodes. Up Next audio downloads over Wi-Fi or cellular and is removed when its episode is archived.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)

                    if model.cachedFiles.isEmpty {
                        ContentUnavailableView(
                            "No cached files",
                            systemImage: "externaldrive",
                            description: Text("Cached artwork and Up Next audio will appear here.")
                        )
                    } else {
                        cachedFileGroup(title: "Audio", files: audioFiles, icon: "waveform")
                        cachedFileGroup(title: "Artwork", files: artworkFiles, icon: "photo")
                    }
                }
            }
            .navigationTitle("Settings")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .task { await model.refreshCachedFiles() }
            .refreshable { await model.refreshCachedFiles() }
        }
    }

    @ViewBuilder
    private func cachedFileGroup(title: String, files: [CachedMediaFile], icon: String) -> some View {
        if !files.isEmpty {
            DisclosureGroup("\(title) · \(files.count)") {
                ForEach(files) { file in
                    HStack(spacing: 12) {
                        Image(systemName: icon)
                            .frame(width: 24)
                            .foregroundStyle(.orange)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.title(forCachedEpisodeID: file.episodeID))
                                .lineLimit(2)
                            Text(file.url.lastPathComponent)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer(minLength: 8)
                        Text(formatBytes(file.byteCount))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        }
    }

    private func formatBytes(_ byteCount: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: byteCount, countStyle: .file)
    }
}
