#!/bin/sh

set -eu

ROOT=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
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
PULSE_ENTITY_ID="macbook-test" \
PULSE_ENTITY_LABEL="Test MacBook" \
PULSE_INTERVAL_SECONDS=300 \
PULSE_DIMENSIONS="environment=test,site=lab" \
sh "$ROOT/scripts/install.sh" \
    --unattended \
    --ingestor="$ingestor" \
    --scheduler=none \
    --prefix="$prefix" \
    --config-path="$config" > "$TMP/install.out"

test -x "$prefix/$binary"
test -f "$config"
test "$(stat -f '%Lp' "$config" 2>/dev/null || stat -c '%a' "$config")" = "600"
grep -Fq 'ingest_url = "https://pulse.example.test/api/pulse/ingest"' "$config"
grep -Fq 'ingest_token = "secret-token"' "$config"
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
    cat > "$command_dir/systemctl" <<'EOF'
#!/bin/sh
set -eu
: "${PULSE_INSTALL_TEST_SYSTEMCTL:?}"
printf '%s\n' "$*" >> "$PULSE_INSTALL_TEST_SYSTEMCTL"
EOF
    chmod +x "$command_dir/systemctl"

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
        --config-path="$config" >/dev/null

    grep -Fq "ExecStart=\"$prefix/pulse-linux\" --config \"$config\" once" "$systemd_dir/pulse-linux.service"
    grep -Fq 'OnUnitActiveSec=120s' "$systemd_dir/pulse-linux.timer"
    grep -Fq 'enable --now pulse-linux.timer' "$TMP/systemctl.log"
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

printf 'installer integration test passed\n'
