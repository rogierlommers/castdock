import SwiftUI

@main
struct CastDockApp: App {
    @StateObject private var model = AppModel()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            Group {
                if model.configuration == nil {
                    SetupView()
                } else {
                    HomeView()
                }
            }
            .environmentObject(model)
            .tint(.orange)
        }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active {
                Task { await model.player.sync() }
            }
        }
    }
}
