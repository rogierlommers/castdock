import SwiftUI
import UIKit

struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    @State private var expandedEpisodeID: Int64?
    @State private var showsSettings = false
    @State private var isSyncing = false
    @State private var showsSyncSuccess = false

    var body: some View {
        NavigationStack {
            List {
                if let error = model.errorMessage {
                    Section { Text(error).foregroundStyle(.red) }
                }
                NowPlayingSection(
                    player: model.player,
                    artworkURL: model.player.episode.flatMap { model.artworkURL(for: $0) },
                    markAsPlayed: { await model.markNowPlayingAsPlayed() }
                )
                Section("Inbox · \(model.inbox.count)") {
                    if model.inbox.isEmpty { ContentUnavailableView("Inbox zero", systemImage: "tray") }
                    ForEach(model.inbox) { episode in
                        ExpandableEpisodeRow(
                            episode: episode,
                            artworkURL: model.artworkURL(for: episode),
                            isExpanded: expansionBinding(for: episode.id),
                            actions: [
                                EpisodeAction(title: "To top of queue", systemImage: "arrow.up.to.line") { Task { await model.queueAtTop(episode) } },
                                EpisodeAction(title: "To bottom of queue", systemImage: "arrow.down.to.line") { Task { await model.queue(episode) } },
                                EpisodeAction(title: "Archive", systemImage: "archivebox") { Task { await model.archive(episode) } },
                            ]
                        )
                    }
                }
                Section("Up Next · \(model.upNext.count)") {
                    if model.upNext.isEmpty { ContentUnavailableView("Nothing queued", systemImage: "text.line.first.and.arrowtriangle.forward") }
                    ForEach(model.upNext) { episode in
                        ExpandableEpisodeRow(
                            episode: episode,
                            artworkURL: model.artworkURL(for: episode),
                            isExpanded: expansionBinding(for: episode.id),
                            actions: [
                                EpisodeAction(title: "Play now", systemImage: "play.fill") { Task { await model.playNow(episode) } },
                                EpisodeAction(title: "Archive", systemImage: "archivebox") { Task { await model.archive(episode) } },
                            ]
                        )
                    }
                    .onMove(perform: model.moveQueue)
                }
            }
            .navigationTitle("CastDock")
            .refreshable { await model.refresh() }
            .toolbar {
                ToolbarItemGroup(placement: .topBarLeading) {
                    EditButton()
                    Button("Sync") {
                        Task {
                            isSyncing = true
                            let succeeded = await model.syncNow()
                            isSyncing = false
                            if succeeded { showsSyncSuccess = true }
                        }
                    }
                    .disabled(isSyncing)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showsSettings = true } label: { Image(systemName: "line.3.horizontal") }
                        .accessibilityLabel("Settings")
                }
            }
            .overlay { if model.isLoading && model.inbox.isEmpty && model.upNext.isEmpty { ProgressView() } }
            .task { await model.refresh() }
            .fullScreenCover(isPresented: $showsSettings) {
                SettingsView().environmentObject(model)
            }
            .alert("Sync succeeded", isPresented: $showsSyncSuccess) {
                Button("OK", role: .cancel) {}
            } message: {
                Text("The server responded successfully and the latest Inbox and Up Next data was loaded.")
            }
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
    let artworkURL: URL?
    @Binding var isExpanded: Bool
    let actions: [EpisodeAction]

    @State private var showsNotes = false

    private let columns = [GridItem(.adaptive(minimum: 40, maximum: 44), spacing: 8)]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                HStack {
                    EpisodeRow(episode: episode, artworkURL: artworkURL)
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
                            Image(systemName: episodeAction.systemImage)
                                .frame(width: 24, height: 24)
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                        .accessibilityLabel(episodeAction.title)
                        .help(episodeAction.title)
                    }
                    Button {
                        showsNotes.toggle()
                    } label: {
                        Image(systemName: showsNotes ? "doc.text.fill" : "doc.text")
                            .frame(width: 24, height: 24)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .accessibilityLabel(showsNotes ? "Hide shownotes" : "Display shownotes")
                    .help(showsNotes ? "Hide shownotes" : "Display shownotes")
                }
                .padding(.leading, EpisodeRow.contentLeadingInset)

                if showsNotes {
                    if let html = episode.descriptionHtml, !html.isEmpty {
                        HTMLShowNotesView(html: html)
                    } else {
                        Text(episode.description.isEmpty ? "No show notes available." : episode.description)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
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
            primaryActionFor textItem: UITextItem,
            defaultAction: UIAction
        ) -> UIAction? {
            guard case .link(let url) = textItem.content else { return defaultAction }
            guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return nil }
            return UIAction { _ in UIApplication.shared.open(url) }
        }
    }
}

private struct NowPlayingSection: View {
    @ObservedObject var player: PlayerController
    let artworkURL: URL?
    let markAsPlayed: () async -> Void

    @State private var showsNotes = false
    @State private var isMarkingPlayed = false

    var body: some View {
        if let episode = player.episode {
            Section("Now Playing") {
                HStack(spacing: 12) {
                    AsyncImage(url: artworkURL) { image in
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
                        let total = player.duration.isFinite ? max(player.duration, 1) : 1
                        let position = player.currentTime.isFinite ? min(max(player.currentTime, 0), total) : 0
                        if let playTimeLeft = PlayTimeFormatter.timeLeft(
                            duration: player.duration,
                            position: player.currentTime,
                            playbackRate: player.playbackRate
                        ) {
                            Text("Play time left: \(playTimeLeft)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        ProgressView(value: position, total: total)
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

                Button {
                    Task {
                        isMarkingPlayed = true
                        await markAsPlayed()
                        isMarkingPlayed = false
                    }
                } label: {
                    Label("Mark as played", systemImage: "checkmark.circle")
                }
                .buttonStyle(.bordered)
                .disabled(isMarkingPlayed)

                DisclosureGroup("Show notes", isExpanded: $showsNotes) {
                    if let html = episode.descriptionHtml, !html.isEmpty {
                        HTMLShowNotesView(html: html)
                    } else {
                        Text(episode.description.isEmpty ? "No show notes available." : episode.description)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
                .onChange(of: episode.id) { _, _ in showsNotes = false }

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
