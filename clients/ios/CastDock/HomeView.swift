import SwiftUI
import UIKit

struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    @State private var expandedEpisodeID: Int64?

    var body: some View {
        NavigationStack {
            List {
                if let error = model.errorMessage {
                    Section { Text(error).foregroundStyle(.red) }
                }
                NowPlayingSection(player: model.player)
                Section("Inbox · \(model.inbox.count)") {
                    if model.inbox.isEmpty { ContentUnavailableView("Inbox zero", systemImage: "tray") }
                    ForEach(model.inbox) { episode in
                        ExpandableEpisodeRow(episode: episode, isExpanded: expansionBinding(for: episode.id), actions: [
                            EpisodeAction(title: "To top of queue", systemImage: "arrow.up.to.line") { Task { await model.queueAtTop(episode) } },
                            EpisodeAction(title: "To bottom of queue", systemImage: "arrow.down.to.line") { Task { await model.queue(episode) } },
                            EpisodeAction(title: "Archive", systemImage: "archivebox") { Task { await model.archive(episode) } },
                        ])
                    }
                }
                Section("Up Next · \(model.upNext.count)") {
                    if model.upNext.isEmpty { ContentUnavailableView("Nothing queued", systemImage: "text.line.first.and.arrowtriangle.forward") }
                    ForEach(model.upNext) { episode in
                        ExpandableEpisodeRow(episode: episode, isExpanded: expansionBinding(for: episode.id), actions: [
                            EpisodeAction(title: "Play now", systemImage: "play.fill") { Task { await model.playNow(episode) } },
                            EpisodeAction(title: "Archive", systemImage: "archivebox") { Task { await model.archive(episode) } },
                        ])
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
            .task { await model.refresh() }
        }
    }

    private func expansionBinding(for episodeID: Int64) -> Binding<Bool> {
        Binding(
            get: { expandedEpisodeID == episodeID },
            set: { expandedEpisodeID = $0 ? episodeID : nil }
        )
    }
}

private struct EpisodeAction: Identifiable {
    let title: String
    let systemImage: String
    let action: () -> Void

    var id: String { title }
}

private struct ExpandableEpisodeRow: View {
    let episode: Episode
    @Binding var isExpanded: Bool
    let actions: [EpisodeAction]

    @State private var showsNotes = false

    private let columns = [GridItem(.adaptive(minimum: 130), spacing: 8)]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                HStack {
                    EpisodeRow(episode: episode)
                    Spacer(minLength: 8)
                    Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(episode.title), \(isExpanded ? "hide" : "show") actions")

            if isExpanded {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 8) {
                    ForEach(actions) { episodeAction in
                        Button(action: episodeAction.action) {
                            Label(episodeAction.title, systemImage: episodeAction.systemImage)
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                    }
                    Button {
                        withAnimation { showsNotes.toggle() }
                    } label: {
                        Label(showsNotes ? "Hide shownotes" : "Display shownotes", systemImage: "doc.text")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }

                if showsNotes {
                    if let html = episode.descriptionHtml, !html.isEmpty {
                        HTMLShowNotesView(html: html)
                            .transition(.opacity)
                    } else {
                        Text(episode.description.isEmpty ? "No show notes available." : episode.description)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                            .transition(.opacity)
                    }
                }
            }
        }
        .padding(.vertical, 2)
        .onChange(of: isExpanded) { _, expanded in
            if !expanded { showsNotes = false }
        }
    }
}

private struct HTMLShowNotesView: UIViewRepresentable {
    let html: String

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> UITextView {
        let textView = UITextView()
        textView.backgroundColor = .clear
        textView.isEditable = false
        textView.isSelectable = true
        textView.isScrollEnabled = false
        textView.textContainerInset = .zero
        textView.textContainer.lineFragmentPadding = 0
        textView.adjustsFontForContentSizeCategory = true
        textView.linkTextAttributes = [
            .foregroundColor: UIColor.link,
            .underlineStyle: NSUnderlineStyle.single.rawValue,
        ]
        textView.delegate = context.coordinator
        textView.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        return textView
    }

    func updateUIView(_ textView: UITextView, context: Context) {
        let renderKey = "\(dynamicTypeSize)-\(html)"
        guard context.coordinator.renderKey != renderKey else { return }
        context.coordinator.renderKey = renderKey

        let showNotes = html.replacingOccurrences(
            of: #"<img\b[^>]*>"#,
            with: "",
            options: [.regularExpression, .caseInsensitive]
        )
        let fontSize = UIFont.preferredFont(forTextStyle: .callout).pointSize
        let document = """
        <style>
        body { font-family: -apple-system; font-size: \(fontSize)px; line-height: 1.35; }
        ul, ol { padding-left: 1.4em; }
        </style>
        \(showNotes)
        """
        guard let attributed = try? NSMutableAttributedString(
            data: Data(document.utf8),
            options: [
                .documentType: NSAttributedString.DocumentType.html,
                .characterEncoding: String.Encoding.utf8.rawValue,
            ],
            documentAttributes: nil
        ) else {
            textView.text = showNotes
            return
        }
        attributed.addAttribute(.foregroundColor, value: UIColor.label, range: NSRange(location: 0, length: attributed.length))
        textView.attributedText = attributed
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: UITextView, context: Context) -> CGSize? {
        guard let width = proposal.width else { return nil }
        let size = uiView.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: width, height: size.height)
    }

    final class Coordinator: NSObject, UITextViewDelegate {
        var renderKey = ""

        func textView(
            _ textView: UITextView,
            shouldInteractWith URL: URL,
            in characterRange: NSRange,
            interaction: UITextItemInteraction
        ) -> Bool {
            guard let scheme = URL.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return false }
            UIApplication.shared.open(URL)
            return false
        }
    }
}

private struct NowPlayingSection: View {
    @ObservedObject var player: PlayerController

    var body: some View {
        if let episode = player.episode {
            Section("Now Playing") {
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
                        ProgressView(value: player.currentTime, total: max(player.duration, 1))
                    }
                }

                HStack {
                    Button { player.seek(by: -15) } label: {
                        Label("Rewind 15 seconds", systemImage: "gobackward.15").labelStyle(.iconOnly).font(.title2)
                    }
                    .accessibilityLabel("Rewind 15 seconds")

                    Spacer()

                    Button { player.togglePlayback() } label: {
                        Label(player.isPlaying ? "Pause" : "Play", systemImage: player.isPlaying ? "pause.circle.fill" : "play.circle.fill")
                            .labelStyle(.iconOnly)
                            .font(.system(size: 44))
                    }
                    .accessibilityLabel(player.isPlaying ? "Pause" : "Play")

                    Spacer()

                    Button { player.seek(by: 15) } label: {
                        Label("Forward 15 seconds", systemImage: "goforward.15").labelStyle(.iconOnly).font(.title2)
                    }
                    .accessibilityLabel("Forward 15 seconds")
                }
                .buttonStyle(.borderless)
                .padding(.horizontal, 24)

                Picker("Playback speed", selection: Binding(
                    get: { player.playbackRate },
                    set: { player.setPlaybackRate($0) }
                )) {
                    ForEach(PlayerController.availablePlaybackRates, id: \.self) { rate in
                        Text(rate.formatted(.number.precision(.fractionLength(0...2))) + "×").tag(rate)
                    }
                }
                .pickerStyle(.menu)

                if let error = player.errorMessage {
                    Text(error).foregroundStyle(.red).font(.callout)
                }
                if let error = player.playbackRateError {
                    Text("Playback speed could not be saved: \(error)").foregroundStyle(.red).font(.callout)
                }
            }
        }
    }
}
