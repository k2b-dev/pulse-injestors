#!/bin/sh

set -eu

ROOT=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
TMP=$(CDPATH='' cd "$TMP" && pwd -P)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) printf 'unsupported test architecture\n' >&2; exit 1 ;;
esac
case "$(uname -s)" in
    Darwin) os=darwin; ingestor=macos; binary=pulse-macos ;;
    Linux) os=linux; ingestor=linux; binary=pulse-linux ;;
    *) printf 'unsupported test operating system\n' >&2; exit 1 ;;
esac

release="$TMP/release"
payload="$TMP/payload"
prefix="$TMP/bin"
config="$TMP/config/ingestor.toml"
calls="$TMP/calls.log"
mkdir -p "$release" "$payload" "$prefix"
export PULSE_INSTALL_NO_SUDO=1
# All scheduler commands stay inside the fixture, including uninstall.
command_dir="$TMP/commands"
mkdir -p "$command_dir" "$TMP/systemd" "$TMP/cron"
export PATH="$command_dir:$PATH"
export PULSE_SYSTEMD_DIR="$TMP/systemd" PULSE_CRON_DIR="$TMP/cron"
export PULSE_INSTALL_TEST_SYSTEMCTL="$TMP/systemctl.log"
cat > "$command_dir/systemctl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$PULSE_INSTALL_TEST_SYSTEMCTL"
if [ "$1" = "is-active" ]; then
    count=0
    [ ! -f "$PULSE_INSTALL_TEST_SYSTEMCTL.count" ] || count=$(cat "$PULSE_INSTALL_TEST_SYSTEMCTL.count")
    count=$((count + 1))
    printf '%s\n' "$count" > "$PULSE_INSTALL_TEST_SYSTEMCTL.count"
    [ "$count" -gt "${PULSE_INSTALL_TEST_WAIT:-0}" ]
fi
EOF
cat > "$command_dir/sleep" <<'EOF'
#!/bin/sh
printf 'sleep %s\n' "$*" >> "$PULSE_INSTALL_TEST_SYSTEMCTL"
EOF
chmod +x "$command_dir/systemctl" "$command_dir/sleep"

cat > "$payload/$binary" <<'EOF'
#!/bin/sh
set -eu
: "${PULSE_INSTALL_TEST_CALLS:?}"
printf '%s\n' "$*" >> "$PULSE_INSTALL_TEST_CALLS"
case " $* " in
  *" --version "*) printf 'pulse test 0.1.0\n' ;;
esac
EOF
chmod +x "$payload/$binary"
cp "$payload/$binary" "$payload/pulse-uptime"

archive="${binary}_${os}_${arch}.tar.gz"
uptime_archive="pulse-uptime_${os}_${arch}.tar.gz"
tar -C "$payload" -czf "$release/$archive" "$binary"
tar -C "$payload" -czf "$release/$uptime_archive" pulse-uptime
if command -v sha256sum >/dev/null 2>&1; then
    checksum=$(sha256sum "$release/$archive" | awk '{print $1}')
    uptime_checksum=$(sha256sum "$release/$uptime_archive" | awk '{print $1}')
else
    checksum=$(shasum -a 256 "$release/$archive" | awk '{print $1}')
    uptime_checksum=$(shasum -a 256 "$release/$uptime_archive" | awk '{print $1}')
fi
printf '%s  %s\n' "$checksum" "$archive" > "$release/checksums.txt"
printf '%s  %s\n' "$uptime_checksum" "$uptime_archive" >> "$release/checksums.txt"
if [ "$os" = linux ]; then
    for extra_binary in pulse-proxmox pulse-proxmox-backup-server; do
        cp "$payload/$binary" "$payload/$extra_binary"
        extra_archive="${extra_binary}_${os}_${arch}.tar.gz"
        tar -C "$payload" -czf "$release/$extra_archive" "$extra_binary"
        sha256sum "$release/$extra_archive" | awk -v file="$extra_archive" '{print $1 "  " file}' >> "$release/checksums.txt"
    done
fi
: > "$release/checksums.txt.sig"
: > "$release/checksums.txt.pem"

cat > "$TMP/cosign" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$TMP/cosign"

token_file="$TMP/token"
printf 'secret-token\n' > "$token_file"
chmod 600 "$token_file"

missing_prefix="$TMP/missing-bin"
if PULSE_RELEASE_BASE="file://$release" \
    PULSE_RELEASE_TAG="v0.1.0" \
    PULSE_COSIGN_BIN="$TMP/cosign" \
    sh "$ROOT/scripts/install.sh" \
        --unattended \
        --ingestor="$ingestor" \
        --scheduler=none \
        --prefix="$missing_prefix" \
        --config-path="$TMP/missing-config.toml" >/dev/null 2>&1; then
    printf 'installer accepted incomplete unattended input\n' >&2
    exit 1
fi
test ! -e "$missing_prefix/$binary"

PULSE_RELEASE_BASE="file://$release" \
PULSE_RELEASE_TAG="v0.1.0" \
PULSE_COSIGN_BIN="$TMP/cosign" \
PULSE_INSTALL_TEST_CALLS="$calls" \
PULSE_INGEST_URL="https://pulse.example.test/api/pulse/ingest" \
PULSE_INGEST_TOKEN_FILE="$token_file" \
PULSE_RESOURCE_ID="macbook-test" \
PULSE_RESOURCE_LABEL="Test MacBook" \
PULSE_INTERVAL_SECONDS=300 \
PULSE_DIMENSIONS="environment=test,site=lab" \
PULSE_SPOOL_DIR="$TMP/spool" \
sh "$ROOT/scripts/install.sh" \
    --unattended \
    --ingestor="$ingestor" \
    --scheduler=none \
    --prefix="$prefix" \
    --config-path="$config" > "$TMP/install.out"

grep -Fq 'next step: set Expected data for this source' "$TMP/install.out"
mkdir -p "$TMP/spool"
printf 'queued data\n' > "$TMP/spool/part.json"
cp "$config" "$TMP/original.toml"
cp "${config%.toml}.token" "$TMP/original.token"
test -x "$prefix/$binary"
test -f "$config"
if [ "$os" = "darwin" ]; then
    config_mode=$(stat -f '%Lp' "$config")
else
    config_mode=$(stat -c '%a' "$config")
fi
test "$config_mode" = "600"
grep -Fq 'ingest_url = "https://pulse.example.test/api/pulse/ingest"' "$config"
token_path="${config%.toml}.token"
grep -Fq "ingest_token_file = \"$token_path\"" "$config"
if grep -Fq 'secret-token' "$config"; then
    printf 'installer wrote the token into the config\n' >&2
    exit 1
fi
test "$(cat "$token_path")" = "secret-token"
if [ "$os" = "darwin" ]; then
    token_mode=$(stat -f '%Lp' "$token_path")
else
    token_mode=$(stat -c '%a' "$token_path")
fi
test "$token_mode" = "600"
grep -Fq '[resource]' "$config"
grep -Fq 'spool_max_bytes = 67108864' "$config"
grep -Fq 'spool_dir = "' "$config"
grep -Fq 'id = "macbook-test"' "$config"
grep -Fq 'label = "Test MacBook"' "$config"
grep -Fq '"environment" = "test"' "$config"
grep -Fq -- '--local once' "$calls"
grep -Fq -- ' once' "$calls"
if grep -Fq 'secret-token' "$TMP/install.out"; then
    printf 'installer leaked token to stdout\n' >&2
    exit 1
fi

uptime_prefix="$TMP/uptime-bin"
uptime_config="$TMP/uptime-config/ingestor.toml"
PULSE_RELEASE_BASE="file://$release" \
PULSE_RELEASE_TAG="v0.1.0" \
PULSE_COSIGN_BIN="$TMP/cosign" \
PULSE_INSTALL_TEST_CALLS="$calls" \
sh "$ROOT/scripts/install.sh" \
    --unattended \
    --ingestor=uptime \
    --scheduler=none \
    --config-source="$config" \
    --prefix="$uptime_prefix" \
    --config-path="$uptime_config" >/dev/null
test -x "$uptime_prefix/pulse-uptime"
test -f "$uptime_config"

if [ "$os" = "linux" ]; then
    systemd_dir="$TMP/systemd"
    cron_dir="$TMP/cron"
    command_dir="$TMP/commands"
    mkdir -p "$systemd_dir" "$cron_dir" "$command_dir"

    PULSE_RELEASE_BASE="file://$release" \
    PULSE_RELEASE_TAG="v0.1.0" \
    PULSE_COSIGN_BIN="$TMP/cosign" \
    PULSE_INSTALL_TEST_CALLS="$calls" \
    PULSE_INSTALL_TEST_SYSTEMCTL="$TMP/systemctl.log" \
    PULSE_SYSTEMD_DIR="$systemd_dir" \
    PULSE_CRON_DIR="$cron_dir" \
    PULSE_INTERVAL_SECONDS=120 \
    PATH="$command_dir:$PATH" \
    sh "$ROOT/scripts/install.sh" \
        --unattended \
        --ingestor=linux \
        --scheduler=systemd \
        --prefix="$prefix" \
        --config-path="$config" > "$TMP/update.out"

    unit="$systemd_dir/pulse-linux.service"
    grep -Fq "ExecStart=\"$prefix/pulse-linux\" --config %d/pulse-config run" "$unit"
    grep -Fq "LoadCredential=pulse-config:$config" "$unit"
    grep -Fq 'Type=notify' "$unit"
    grep -Fq 'WatchdogSec=120s' "$unit"
    grep -Fq "LoadCredential=pulse-ingest-token:$token_path" "$unit"
    grep -Fq 'Environment=PULSE_INGEST_TOKEN_FILE=%d/pulse-ingest-token' "$unit"
    grep -Fq 'StateDirectory=pulse/pulse-linux' "$unit"
    grep -Fq 'NoNewPrivileges=yes' "$unit"
    test ! -e "$systemd_dir/pulse-linux.timer"
    grep -Fq 'enable pulse-linux.service' "$TMP/systemctl.log"
    grep -Fq 'restart --no-block pulse-linux.service' "$TMP/systemctl.log"

    grep -Fq 'is-active --quiet pulse-linux.service' "$TMP/systemctl.log"
    grep -Fq 'pulse-linux.service is active' "$TMP/update.out"
    grep -Fq 'update complete; configuration and token preserved' "$TMP/update.out"
    cmp "$config" "$TMP/original.toml"
    cmp "$token_path" "$TMP/original.token"
    test "$(grep -Fc -- "--config $config once" "$calls")" -eq 1
    cmp "$prefix/$binary" "$payload/$binary"

    grep -Fq 'MemoryMax=256M' "$unit"
    # A leftover managed token must not override inline or foreign credentials.
    for credential_kind in inline foreign; do
        if [ "$credential_kind" = inline ]; then
            sed 's|^ingest_token_file = .*|ingest_token = "inline-fixture"|' "$TMP/original.toml" > "$config"
        else
            sed "s|^ingest_token_file = .*|ingest_token_file = \"$token_file\"|" "$TMP/original.toml" > "$config"
        fi
        PULSE_RELEASE_BASE="file://$release" PULSE_RELEASE_TAG=v0.1.0 \
        PULSE_COSIGN_BIN="$TMP/cosign" PULSE_INSTALL_TEST_CALLS="$calls" \
        sh "$ROOT/scripts/install.sh" --unattended --ingestor=linux \
            --scheduler=systemd --prefix="$prefix" --config-path="$config" >/dev/null
        if grep -Eq '^(LoadCredential=pulse-ingest-token:|Environment=PULSE_INGEST_TOKEN_FILE=)' "$unit"; then
            printf 'overrode %s credential\n' "$credential_kind" >&2; exit 1
        fi
    done
    cp "$TMP/original.toml" "$config"
    for heavy_ingestor in proxmox pbs; do
        heavy_binary=pulse-proxmox
        [ "$heavy_ingestor" != pbs ] || heavy_binary=pulse-proxmox-backup-server
        PULSE_RELEASE_BASE="file://$release" PULSE_RELEASE_TAG=v0.1.0 \
        PULSE_COSIGN_BIN="$TMP/cosign" PULSE_INSTALL_TEST_CALLS="$calls" \
        sh "$ROOT/scripts/install.sh" --unattended --ingestor="$heavy_ingestor" \
            --scheduler=systemd --prefix="$prefix" --config-source="$config" \
            --config-path="$TMP/$heavy_binary.toml" >/dev/null
        grep -Fq 'MemoryMax=512M' "$systemd_dir/$heavy_binary.service"
    done

    uptime_systemd_config="$TMP/uptime-systemd/pulse-uptime.toml"
    PULSE_RELEASE_BASE="file://$release" \
    PULSE_RELEASE_TAG="v0.1.0" \
    PULSE_COSIGN_BIN="$TMP/cosign" \
    PULSE_INSTALL_TEST_CALLS="$calls" \
    PULSE_INSTALL_TEST_SYSTEMCTL="$TMP/systemctl.log" \
    PULSE_SYSTEMD_DIR="$systemd_dir" \
    PULSE_CRON_DIR="$cron_dir" \
    PULSE_INGEST_URL="https://pulse.example.test/api/pulse/ingest" \
    PULSE_INGEST_TOKEN_FILE="$token_file" \
    PULSE_RESOURCE_ID="probe-test" \
    PATH="$command_dir:$PATH" \
    sh "$ROOT/scripts/install.sh" \
        --unattended \
        --ingestor=uptime \
        --scheduler=systemd \
        --prefix="$uptime_prefix" \
        --config-path="$uptime_systemd_config" >/dev/null
    grep -Fq "LoadCredential=pulse-config:$uptime_systemd_config" "$systemd_dir/pulse-uptime.service"
    grep -Fq "ExecStart=\"$uptime_prefix/pulse-uptime\" --config %d/pulse-config run" "$systemd_dir/pulse-uptime.service"
    grep -Fq 'WatchdogSec=120s' "$systemd_dir/pulse-uptime.service"
    grep -Fq 'DynamicUser=yes' "$systemd_dir/pulse-uptime.service"
    grep -Fxq 'AmbientCapabilities=CAP_NET_RAW' "$systemd_dir/pulse-uptime.service"
    grep -Fxq 'CapabilityBoundingSet=CAP_NET_RAW' "$systemd_dir/pulse-uptime.service"
    grep -Fq 'ProtectSystem=strict' "$systemd_dir/pulse-uptime.service"
fi

output=$(
    PULSE_RELEASE_BASE="file://$release" \
    PULSE_RELEASE_TAG="v0.1.0" \
    PULSE_COSIGN_BIN="$TMP/cosign" \
    PULSE_INSTALL_TEST_CALLS="$calls" \
    sh "$ROOT/scripts/install.sh" \
        --unattended \
        --uninstall \
        --ingestor="$ingestor" \
        --scheduler=none \
        --prefix="$prefix" \
        --config-path="$config"
)
test ! -e "$prefix/$binary"
test -f "$config"
printf '%s\n' "$output" | grep -Fq 'preserved configuration'

PULSE_RELEASE_BASE="file://$release" \
PULSE_RELEASE_TAG="v0.1.0" \
PULSE_COSIGN_BIN="$TMP/cosign" \
PULSE_INSTALL_TEST_CALLS="$calls" \
sh "$ROOT/scripts/install.sh" \
    --unattended \
    --uninstall \
    --ingestor=uptime \
    --scheduler=none \
    --prefix="$uptime_prefix" \
    --config-path="$uptime_config" >/dev/null
test ! -e "$uptime_prefix/pulse-uptime"
test -f "$uptime_config"

# Exercise the fixed macOS managed path under a private HOME on every platform.
# No purge test may touch the machine's real /var/lib/pulse state.
mkdir -p "$TMP/purge-commands" "$TMP/home"
cat > "$TMP/purge-commands/uname" <<'EOF'
#!/bin/sh
if [ "$1" = -s ]; then printf 'Darwin\n'; else /usr/bin/uname "$@"; fi
EOF
cat > "$TMP/purge-commands/launchctl" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$TMP/purge-commands/uname" "$TMP/purge-commands/launchctl"
purge_fixture() {
    HOME="$TMP/home" PATH="$TMP/purge-commands:$PATH" sh "$ROOT/scripts/install.sh" \
        --unattended --uninstall --purge --ingestor=macos --scheduler=none \
        --prefix="$prefix" --config-path="$config"
}
managed_spool="$TMP/home/Library/Application Support/Pulse/pulse-macos-spool"
mkdir -p "$managed_spool/quarantine" "$managed_spool/unknown.part"
for entry in a.part .states .binding .lock .pending-test kernel.cursor quarantine/rejected.json unknown.txt .unknown quarantine/keep.txt; do
    printf 'fixture\n' > "$managed_spool/$entry"
done
# The installed config points at TMP/spool: not a single byte there is removed.
purge_fixture > "$TMP/purge.out"
test -f "$config"
test ! -e "$token_path"
test -f "$TMP/spool/part.json"
for entry in a.part .states .binding .lock .pending-test kernel.cursor quarantine/rejected.json; do
    test ! -e "$managed_spool/$entry"
done
for entry in unknown.txt .unknown quarantine/keep.txt; do
    test -f "$managed_spool/$entry"
    grep -Fq "$managed_spool/$entry" "$TMP/purge.out"
done
test -d "$managed_spool/unknown.part"
grep -Fq 'left unrecognized spool entries' "$TMP/purge.out"
grep -Fq 'purged managed spool entries and token file' "$TMP/purge.out"
rm -rf "$managed_spool"
mkdir -p "$managed_spool/quarantine"
printf 'known\n' > "$managed_spool/quarantine/rejected.json"
purge_fixture >/dev/null
test ! -e "$managed_spool"

# Config-derived paths, even unsafe-looking ones, never become purge targets.
cp "$config" "$TMP/purge-config-before.toml"
mkdir -p "$TMP/foreign-spool"
printf 'keep\n' > "$TMP/foreign-spool/.states"
printf 'keep\n' > "$TMP/foreign.token"
ln -s "$TMP/foreign-spool" "$TMP/spool-link"
for configured_spool in / /var/lib "$TMP/../spool" "$TMP/spool-link"; do
    printf '[pulse]\nspool_dir = "%s"\ningest_token_file = "%s"\n' \
        "$configured_spool" "$TMP/foreign.token" > "$config"
    purge_fixture >/dev/null
    test -f "$TMP/foreign-spool/.states"
    test -L "$TMP/spool-link"
    test -f "$TMP/foreign.token"
done
cp "$TMP/purge-config-before.toml" "$config"

# Refuse directory, entry, quarantine, parent, and managed-token symlinks.
for link in "$managed_spool" "$managed_spool/a.part" "$managed_spool/quarantine" "$token_path"; do
    mkdir -p "$(dirname "$link")"
    ln -s "$TMP/foreign-spool" "$link"
    if purge_fixture > "$TMP/invalid.out" 2>&1; then
        printf 'accepted symlink purge target: %s\n' "$link" >&2; exit 1
    fi
    grep -Fq 'refusing symlink' "$TMP/invalid.out"
    test -f "$TMP/foreign-spool/.states"
    test -L "$link"
    rm "$link"
done
rm -rf "$managed_spool"
managed_parent=$(dirname "$managed_spool")
rmdir "$managed_parent"
ln -s "$TMP/foreign-spool" "$managed_parent"
if purge_fixture > "$TMP/invalid.out" 2>&1; then
    printf 'accepted symlink parent\n' >&2; exit 1
fi
rm "$managed_parent"

if sh "$ROOT/scripts/install.sh" --purge > "$TMP/invalid.out" 2>&1; then
    printf 'accepted purge without uninstall\n' >&2; exit 1
fi
grep -Fq -- '--purge requires --uninstall' "$TMP/invalid.out"

if [ "$os" = "linux" ]; then
    export PULSE_RELEASE_BASE="file://$release" PULSE_RELEASE_TAG=v0.1.0
    export PULSE_COSIGN_BIN="$TMP/cosign" PULSE_INSTALL_TEST_CALLS="$calls"
    export PULSE_INGEST_URL=https://pulse.example.test/api/pulse/ingest
    export PULSE_INGEST_TOKEN_FILE="$token_file" PULSE_RESOURCE_ID=server-test
    export PULSE_SPOOL_DIR="$TMP/profile-spool"
    # Take profile names from the binary so future additions cannot drift silently.
    profiles=$(sed -n '/func profileSystemdUnits(/,/^}/p' "$ROOT/cmd/pulse-linux/main.go" |
        sed -n 's/.*case "\([^"]*\)":/\1/p')
    for profile in $profiles none; do
        sh "$ROOT/scripts/install.sh" --unattended --reconfigure --ingestor=linux \
            --linux-profile="$profile" --scheduler=none --prefix="$prefix" \
            --config-path="$TMP/profile.toml" > "$TMP/profile.out"
        grep -Fq "profile = \"$profile\"" "$TMP/profile.toml"
    done
    for profile in invalid ''; do
        if sh "$ROOT/scripts/install.sh" --unattended --ingestor=linux \
            --linux-profile="$profile" --prefix="$TMP/invalid-bin" \
            --config-path="$TMP/invalid.toml" > "$TMP/invalid.out" 2>&1; then
            printf 'accepted invalid Linux profile\n' >&2; exit 1
        fi
        test ! -e "$TMP/invalid-bin"
        test ! -e "$TMP/invalid.toml"
    done
    if sh "$ROOT/scripts/install.sh" --unattended --ingestor=uptime \
        --linux-profile=server > "$TMP/invalid.out" 2>&1; then
        printf 'accepted Linux profile for uptime\n' >&2; exit 1
    fi
    if sh "$ROOT/scripts/install.sh" --unattended --ingestor=linux \
        --linux-profile=server --config-source="$config" > "$TMP/invalid.out" 2>&1; then
        printf 'accepted ambiguous config source/profile\n' >&2; exit 1
    fi

    # Replace a different binary, preserve the desktop config and private token,
    # and wait through an activating service without real sleeps.
    sh "$ROOT/scripts/install.sh" --unattended --reconfigure --ingestor=linux \
        --linux-profile=desktop --scheduler=none --prefix="$prefix" \
        --config-path="$TMP/profile.toml" >/dev/null
    cp "$TMP/profile.toml" "$TMP/profile-before.toml"
    cp "$TMP/profile.token" "$TMP/token-before"
    printf '#!/bin/sh\nexit 1\n' > "$prefix/$binary"
    rm -f "$PULSE_INSTALL_TEST_SYSTEMCTL.count"
    PULSE_INSTALL_TEST_WAIT=2 sh "$ROOT/scripts/install.sh" --unattended \
        --ingestor=linux --linux-profile=server --scheduler=systemd --prefix="$prefix" \
        --config-path="$TMP/profile.toml" > "$TMP/wait.out"
    cmp "$prefix/$binary" "$payload/$binary"
    cmp "$TMP/profile.toml" "$TMP/profile-before.toml"
    cmp "$TMP/profile.token" "$TMP/token-before"
    test "$(cat "$PULSE_INSTALL_TEST_SYSTEMCTL.count")" -eq 3
    grep -Fq 'is active' "$TMP/wait.out"

    rm -f "$PULSE_INSTALL_TEST_SYSTEMCTL.count"
    if PULSE_INSTALL_TEST_WAIT=100 sh "$ROOT/scripts/install.sh" --unattended \
        --ingestor=linux --scheduler=systemd --prefix="$prefix" \
        --config-path="$TMP/profile.toml" > "$TMP/timeout.out" 2>&1; then
        printf 'accepted service readiness timeout\n' >&2; exit 1
    fi
    test "$(cat "$PULSE_INSTALL_TEST_SYSTEMCTL.count")" -eq 61
    grep -Fq 'did not become active within 60 s' "$TMP/timeout.out"
    if grep -Fq 'update complete' "$TMP/timeout.out"; then exit 1; fi
fi

printf 'installer integration test passed\n'
