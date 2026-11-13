# CastDock for iOS

Native SwiftUI client for the CastDock self-hosted podcast server.

## Open in Xcode

Open `CastDock.xcodeproj`, select the `CastDock` scheme and an iPhone simulator, then run. The project targets iOS 17 and has no third-party dependencies.

The first launch asks for a server URL and optional CastDock credentials. Connection details are stored in Keychain, and the active hostname is shown in Settings. HTTP servers are supported for private self-hosting, but HTTPS is strongly recommended because credentials, session cookies, and playback state otherwise travel in plaintext. If a backend restart invalidates its in-memory session, the client automatically signs in again and retries the protected request.

## MVP scope

- Expandable Inbox and Up Next rows with play-time-left estimates and inline queue, play, archive, and show-note actions
- Native AVPlayer streaming with a Home-screen Now Playing section for play/pause, live play-time-left, ±15-second seeking, and subscription playback speed
- Five-second playback synchronization
- Immediate Up Next removal after a completed playback update is accepted by the server
- The loaded Now Playing episode is hidden from Up Next before completion without changing its server-owned queue state
- Manual Sync action with confirmation only after the latest Home response is received, decoded, and applied
- Cached artwork for active episodes and automatic Up Next audio downloads over Wi-Fi or cellular
- Offline playback from cached audio, with cached artwork and audio deleted when an episode is archived
- A Settings cache overview listing every cached file, its episode, and its size
- Background audio, Now Playing metadata and lock-screen controls

Library management, Archive, Recent Published, Play History and OPML remain web-only in this milestone.
