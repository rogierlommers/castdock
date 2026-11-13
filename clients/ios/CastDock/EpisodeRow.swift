import SwiftUI

struct EpisodeRow: View {
    private static let artworkSize: CGFloat = 56
    private static let rowSpacing: CGFloat = 12
    static let contentLeadingInset = artworkSize + rowSpacing

    let episode: Episode
    let artworkURL: URL?

    var body: some View {
        HStack(spacing: Self.rowSpacing) {
            AsyncImage(url: artworkURL) { image in
                image.resizable().scaledToFill()
            } placeholder: {
                ZStack { Color.secondary.opacity(0.15); Image(systemName: "waveform").foregroundStyle(.orange) }
            }
            .frame(width: Self.artworkSize, height: Self.artworkSize)
            .clipShape(RoundedRectangle(cornerRadius: 9))
            VStack(alignment: .leading, spacing: 4) {
                Text(episode.title).font(.headline).lineLimit(2)
                Text(episode.podcastTitle).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                if let playTimeLeft = episode.playTimeLeft {
                    Text("Play time left: \(playTimeLeft)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                if episode.positionSeconds > 0 && !episode.completed {
                    let total = max(Double(episode.durationSeconds), 1)
                    ProgressView(value: min(episode.positionSeconds, total), total: total)
                }
            }
        }
    }
}
