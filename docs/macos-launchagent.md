# macOS LaunchAgent

The native installer creates and manages the macOS LaunchAgent automatically:

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=macos
```

It installs:

- `/usr/local/bin/pulse-macos`
- `~/Library/Application Support/Pulse/ingestor.toml`
- `~/Library/LaunchAgents/dev.pulse.pulse-macos.plist`
- logs under `~/Library/Logs/Pulse/`

Run the installer as the target user, not as root. It requests `sudo` only while installing the system binary.

The complete user guide is [`docs/en/ingestor-macos.md`](en/ingestor-macos.md).
