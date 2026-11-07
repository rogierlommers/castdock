import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published var configuration: ServerConfiguration?
    @Published var inbox: [Episode] = []
    @Published var upNext: [Episode] = []
    @Published var isLoading = false
    @Published var errorMessage: String?

    let player = PlayerController()

    private let keychain = KeychainStore()
    private(set) var apiClient: APIClient?

    init() {
        configuration = keychain.load()
        if let configuration { apiClient = APIClient(configuration: configuration) }
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
        guard let apiClient else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            apply(try await apiClient.home())
            errorMessage = nil
        } catch APIError.unauthorized {
            do {
                try await apiClient.login()
                apply(try await apiClient.home())
            } catch {
                errorMessage = error.localizedDescription
                keychain.clear()
                self.configuration = nil
                self.apiClient = nil
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func queue(_ episode: Episode) async { await mutate { try await $0.queue(id: episode.id) } }
    func archive(_ episode: Episode) async { await mutate { try await $0.archive(id: episode.id) } }
    func markPlayed(_ episode: Episode) async { await mutate { try await $0.markPlayed(id: episode.id) } }

    func moveQueue(from offsets: IndexSet, to destination: Int) {
        guard let source = offsets.first, source != destination, source < upNext.count else { return }
        let episode = upNext[source]
        upNext.move(fromOffsets: offsets, toOffset: destination)
        let target = (upNext.firstIndex(where: { $0.id == episode.id }) ?? source) + 1
        Task {
            do {
                try await apiClient?.reorder(id: episode.id, position: target)
            } catch {
                errorMessage = error.localizedDescription
            }
            await refresh()
        }
    }

    func disconnect() {
        player.stop()
        keychain.clear()
        HTTPCookieStorage.shared.cookies?.forEach(HTTPCookieStorage.shared.deleteCookie)
        configuration = nil
        apiClient = nil
        inbox = []
        upNext = []
    }

    private func mutate(_ operation: (APIClient) async throws -> Void) async {
        guard let apiClient else { return }
        do {
            try await operation(apiClient)
            await refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func apply(_ home: HomeResponse) {
        inbox = home.inbox
        upNext = home.upNext
    }
}
