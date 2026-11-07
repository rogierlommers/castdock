import SwiftUI

struct EpisodeRow: View {
    let episode: Episode

    var body: some View {
        HStack(spacing: 12) {
            AsyncImage(url: episode.imageURL) { image in
                image.resizable().scaledToFill()
            } placeholder: {
                ZStack { Color.secondary.opacity(0.15); Image(systemName: "waveform").foregroundStyle(.orange) }
            }
            .frame(width: 56, height: 56)
            .clipShape(RoundedRectangle(cornerRadius: 9))
            VStack(alignment: .leading, spacing: 4) {
                Text(episode.title).font(.headline).lineLimit(2)
                Text(episode.podcastTitle).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                if episode.positionSeconds > 0 && !episode.completed {
                    ProgressView(value: episode.positionSeconds, total: max(Double(episode.durationSeconds), 1))
                }
            }
        }
    }
}
