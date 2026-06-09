# macOS LaunchAgent

Use this LaunchAgent to run `pulse-macos` as a scheduled one-shot client monitor.

1. Install the binary at `/usr/local/bin/pulse-macos`.
2. Put the Pulse config at `/etc/pulse/ingestor.toml`.
3. Copy `configs/launchd/com.valentinkolb.pulse-macos.plist` to `~/Library/LaunchAgents/`.
4. Load it:

```sh
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/com.valentinkolb.pulse-macos.plist
launchctl enable "gui/$(id -u)/com.valentinkolb.pulse-macos"
```

The template runs once at load and then every 300 seconds. Keep secrets in the TOML config, not in the plist. Logs go to `/tmp/pulse-macos.out.log` and `/tmp/pulse-macos.err.log`.

Validate locally before loading:

```sh
/usr/local/bin/pulse-macos --config /etc/pulse/ingestor.toml --local --pretty once
plutil -lint ~/Library/LaunchAgents/com.valentinkolb.pulse-macos.plist
```
