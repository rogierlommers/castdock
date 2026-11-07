import SwiftUI

struct SetupView: View {
    @EnvironmentObject private var model: AppModel
    @State private var serverURL = ""
    @State private var username = ""
    @State private var password = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("CastDock server") {
                    TextField("http://castdock.local:8080", text: $serverURL)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                        .autocorrectionDisabled()
                    Text("Use HTTPS whenever the server is reachable outside a trusted private network.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Section("Optional login") {
                    TextField("Username", text: $username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    SecureField("Password", text: $password)
                }
                if let error = model.errorMessage {
                    Section { Text(error).foregroundStyle(.red) }
                }
                Section {
                    Button {
                        Task { await model.connect(serverURL: serverURL, username: username, password: password) }
                    } label: {
                        if model.isLoading { ProgressView().frame(maxWidth: .infinity) }
                        else { Text("Connect").frame(maxWidth: .infinity) }
                    }
                    .disabled(serverURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.isLoading)
                }
            }
            .navigationTitle("Connect to CastDock")
        }
    }
}
