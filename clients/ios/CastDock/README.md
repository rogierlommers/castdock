# CastDock for iOS

Native SwiftUI client for the CastDock self-hosted podcast server.

## Open in Xcode

Open `CastDock.xcodeproj`, select the `CastDock` scheme and an iPhone simulator, then run. The project targets iOS 17 and has no third-party dependencies.

The first launch asks for a server URL and optional CastDock credentials. Connection details are stored in Keychain. HTTP servers are supported for private self-hosting, but HTTPS is strongly recommended because credentials, session cookies, and playback state otherwise travel in plaintext.

## MVP scope

- Inbox and Up Next
- Queue, archive, and mark-played actions
- Native AVPlayer streaming with subscription playback speed and ±15-second seeking
- Five-second playback synchronization
- Background audio, Now Playing metadata and lock-screen controls

Library management, Archive, Recent Published, Play History and OPML remain web-only in this milestone.
