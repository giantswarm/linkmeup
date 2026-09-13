# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

- TUI: press `Enter` on a selected installation to open a detail view explaining its health, including the last check result (HTTP status, error, latency, age), the Teleport nodes and active node, the node lookup error behind `- No Nodes`, and a log of recent events like failed checks and tunnel restarts.

### Changed

- Release binaries now include darwin/amd64, darwin/arm64, windows/amd64, and windows/arm64 alongside the existing linux targets. Windows binaries are named `template-windows-<arch>.exe`.
- Restart attempts for a failing proxy now back off, from 30 seconds up to 15 minutes, instead of repeating every 30 seconds indefinitely. Each attempt runs `tsh ssh`, which opens a browser tab for Teleport re-authentication when it fails, so a permanently broken installation used to produce roughly 120 tabs an hour.

### Fixed

- An installation whose control plane nodes are replaced now recovers. The node list was previously read once at startup and never refreshed, so every restart kept targeting nodes that no longer existed.
- The node lookup no longer treats anything `tsh` writes to stderr as a failure. A warning alongside a successful lookup used to discard the result.
- The node lookup is bounded by a 30 second timeout and stops when linkmeup shuts down, so a `tsh` waiting on a browser login cannot hold up quitting.
- A restart now really does move to the node `selectNode` picked. `Start` chose a random node instead, which could land on the node that had just failed.
- A proxy that starts with no nodes available now keeps looking for them, rather than staying idle until linkmeup is restarted.

- Health checks no longer leak a goroutine and a socket per check when a tunnel accepts connections but stalls during the SOCKS5 handshake. The dial now honors the context and times out after 10 seconds.
- Pooled connections are dropped when a tunnel is stopped, instead of lingering until they are used again.
- Stopping a tunnel now kills the whole `tsh` process group on Unix, so child processes no longer survive as orphans. On Windows, only the `tsh` process itself is killed, as before.
- Quitting no longer leaves a tunnel behind when a health check restarts a proxy during shutdown, or when starting one proxy fails after others are already running.

## [0.5.0] - 2026-04-01

### Changed

- Update bubbletea to v2, and other dependency updates

## [0.4.0] - 2025-12-05

### Changed

- Replaced log-based output with interactive TUI (Terminal User Interface) using Bubble Tea.
- Main view now shows a table of all configured installations with their status.

## [0.3.1] - 2025-07-10

### Fixed

- Fixed error handling in case no Teleport nodes are available for an installation.

## [0.3.0] - 2025-07-01

### Added

- Checks Teleport login status at startup and presents info to user if not logged in.

## [0.2.0] - 2025-06-11

### Changed

- Proxies get recreated in case of an unsuccessful ping with a different backend node
- Log output is now colored for better readability

## [0.1.0] - 2025-06-10

- First implementation

[Unreleased]: https://github.com/giantswarm/linkmeup/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/giantswarm/linkmeup/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/giantswarm/linkmeup/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/giantswarm/linkmeup/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/giantswarm/linkmeup/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/giantswarm/linkmeup/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/giantswarm/linkmeup/releases/tag/v0.1.0
