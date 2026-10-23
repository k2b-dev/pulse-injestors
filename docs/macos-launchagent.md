# macOS LaunchAgent

The native installer creates and manages the macOS LaunchAgent automatically:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=macos
```

It installs:

- `/usr/local/bin/pulse-macos`
- `~/Library/Application Support/Pulse/pulse-macos.toml` and the token file `pulse-macos.token` next to it
- `~/Library/LaunchAgents/dev.pulse.pulse-macos.plist`, which keeps `pulse-macos run` running
- logs under `~/Library/Logs/Pulse/`

Run the installer as the target user, not as root. It requests `sudo` only while installing the system binary.

The complete user guide is [`docs/en/ingestor-macos.md`](en/ingestor-macos.md).
