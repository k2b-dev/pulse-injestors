#!/bin/sh

set -eu

REPO="${PULSE_INSTALL_REPO:-k2b-dev/pulse-injestors}"
GITHUB_BASE="${PULSE_GITHUB_BASE:-https://github.com/${REPO}}"
VERSION="${PULSE_INSTALL_VERSION:-latest}"
INGESTOR="auto"
SCHEDULER="auto"
PREFIX="${PULSE_INSTALL_PREFIX:-/usr/local/bin}"
CONFIG_SOURCE=""
CONFIG_PATH="${PULSE_CONFIG_PATH:-}"
UNATTENDED=0
RECONFIGURE=0
UNINSTALL=0
ASSUME_YES=0
COSIGN="${PULSE_COSIGN_BIN:-cosign}"

usage() {
    cat <<'EOF'
Usage: install.sh [options]

Install, configure, verify, and start one native Pulse ingestor as a
service. Docker uses the separate Compose deployment and is not supported here.

  --ingestor=NAME       auto, linux, macos, proxmox, pbs, or uptime
                        For proxmox/pbs, install pulse-linux separately too.
  --scheduler=NAME      auto, systemd, cron, launchd, or none
                        (systemd and launchd run a supervised service; cron
                        runs one collection per interval without cadences)
  --config-source=PATH  install an existing complete TOML config
  --config-path=PATH    destination config path (default per ingestor,
                        for example /etc/pulse/pulse-linux.toml); the token
                        is stored next to it with the suffix .token
  --prefix=DIR          binary directory (default: /usr/local/bin)
  --version=VERSION     release version, for example v0.1.0
  --unattended          never prompt; fail when required input is missing
  --reconfigure         replace an existing managed config
  --uninstall           remove the binary and managed scheduler
  -y, --yes             accept confirmation prompts
  -h, --help            show this help

Unattended generated configuration:
  PULSE_INGEST_URL
  PULSE_INGEST_TOKEN_FILE (recommended) or PULSE_INGEST_TOKEN
  PULSE_RESOURCE_ID
  PULSE_RESOURCE_LABEL (optional; defaults to resource id)
  PULSE_INTERVAL_SECONDS (optional; platform default)
  PULSE_DIMENSIONS (optional comma-separated key=value pairs)
  PULSE_SPOOL_DIR (optional; private persistent delivery directory)
  PULSE_SPOOL_MAX_BYTES (optional; default 67108864, a 64 MiB disk budget)

Installer testing and mirrors:
  PULSE_RELEASE_BASE
  PULSE_RELEASE_TAG
  PULSE_COSIGN_BIN
EOF
}

die() {
    printf 'pulse-install: %s\n' "$*" >&2
    exit 1
}

log() {
    printf 'pulse-install: %s\n' "$*"
}

have() {
    command -v "$1" >/dev/null 2>&1
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --ingestor=*) INGESTOR=${1#*=} ;;
        --scheduler=*) SCHEDULER=${1#*=} ;;
        --config-source=*) CONFIG_SOURCE=${1#*=} ;;
        --config-path=*) CONFIG_PATH=${1#*=} ;;
        --prefix=*) PREFIX=${1#*=} ;;
        --version=*) VERSION=${1#*=} ;;
        --unattended) UNATTENDED=1; ASSUME_YES=1 ;;
        --reconfigure) RECONFIGURE=1 ;;
        --uninstall) UNINSTALL=1; ASSUME_YES=1 ;;
        -y|--yes) ASSUME_YES=1 ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown option: $1" ;;
    esac
    shift
done

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$OS" in
    linux|darwin) ;;
    *) die "unsupported operating system: $OS" ;;
esac
case "$ARCH" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "unsupported architecture: $ARCH" ;;
esac

if [ "$UNATTENDED" = "1" ] && [ "$INGESTOR" = "auto" ]; then
    die "--unattended requires an explicit --ingestor"
fi

detect_ingestor() {
    if [ "$OS" = "darwin" ]; then
        printf 'macos'
    elif have pvesh; then
        printf 'proxmox'
    elif have proxmox-backup-debug; then
        printf 'pbs'
    else
        printf 'linux'
    fi
}

if [ "$INGESTOR" = "auto" ]; then
    INGESTOR=$(detect_ingestor)
fi

case "$INGESTOR" in
    linux)
        [ "$OS" = "linux" ] || die "pulse-linux requires Linux"
        BINARY="pulse-linux"
        DEFAULT_INTERVAL=60
        ;;
    macos)
        [ "$OS" = "darwin" ] || die "pulse-macos requires macOS"
        BINARY="pulse-macos"
        DEFAULT_INTERVAL=300
        ;;
    proxmox)
        [ "$OS" = "linux" ] || die "pulse-proxmox requires Linux"
        BINARY="pulse-proxmox"
        DEFAULT_INTERVAL=60
        ;;
    pbs)
        [ "$OS" = "linux" ] || die "pulse-proxmox-backup-server requires Linux"
        BINARY="pulse-proxmox-backup-server"
        DEFAULT_INTERVAL=60
        ;;
    uptime)
        BINARY="pulse-uptime"
        DEFAULT_INTERVAL=60
        ;;
    docker)
        die "Docker is installed with the published Compose deployment, not this script"
        ;;
    *)
        die "unknown ingestor: $INGESTOR"
        ;;
esac

if [ -z "$CONFIG_PATH" ]; then
    if [ "$OS" = "darwin" ]; then
        CONFIG_PATH="${HOME}/Library/Application Support/Pulse/${BINARY}.toml"
    else
        CONFIG_PATH="/etc/pulse/${BINARY}.toml"
    fi
fi
TOKEN_PATH="${CONFIG_PATH%.toml}.token"

BIN_PATH="${PREFIX}/${BINARY}"
SERVICE_ID="$BINARY"
CREDENTIAL_NAME="pulse-ingest-token"
PLIST_LABEL="dev.pulse.${BINARY}"
SYSTEMD_DIR="${PULSE_SYSTEMD_DIR:-/etc/systemd/system}"
CRON_DIR="${PULSE_CRON_DIR:-/etc/cron.d}"

run_root() {
    if [ "${PULSE_INSTALL_NO_SUDO:-0}" = "1" ] || [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        have sudo || die "sudo is required to write system files"
        sudo "$@"
    fi
}

path_needs_root() {
    case "$1" in
        /etc/*|/usr/*|/Library/*) return 0 ;;
        *) return 1 ;;
    esac
}

install_root_file() {
    source_file=$1
    destination=$2
    mode=$3
    if path_needs_root "$destination"; then
        run_root install -d -m 0755 "$(dirname "$destination")"
        run_root install -m "$mode" "$source_file" "$destination"
    else
        install -d -m 0755 "$(dirname "$destination")"
        install -m "$mode" "$source_file" "$destination"
    fi
}

remove_root_file() {
    if path_needs_root "$1"; then
        run_root rm -f "$1"
    else
        rm -f "$1"
    fi
}

config_is_user_file() {
    case "$CONFIG_PATH" in
        "$HOME"/*) return 0 ;;
        *) return 1 ;;
    esac
}

install_config_file() {
    source_file=$1
    if config_is_user_file || ! path_needs_root "$CONFIG_PATH"; then
        install -d -m 0700 "$(dirname "$CONFIG_PATH")"
        install -m 0600 "$source_file" "$CONFIG_PATH"
    else
        run_root install -d -m 0750 "$(dirname "$CONFIG_PATH")"
        run_root install -m 0600 "$source_file" "$CONFIG_PATH"
    fi
}

install_token_file() {
    source_file=$1
    if config_is_user_file || ! path_needs_root "$TOKEN_PATH"; then
        install -d -m 0700 "$(dirname "$TOKEN_PATH")"
        install -m 0600 "$source_file" "$TOKEN_PATH"
    else
        run_root install -d -m 0750 "$(dirname "$TOKEN_PATH")"
        run_root install -m 0600 "$source_file" "$TOKEN_PATH"
    fi
}

token_file_exists() {
    if path_needs_root "$TOKEN_PATH" && ! config_is_user_file; then
        run_root test -f "$TOKEN_PATH"
    else
        test -f "$TOKEN_PATH"
    fi
}

# The enrollment check uses a throwaway spool so the service starts with its
# own state directory and sends every state once.
run_ingestor() {
    if [ "$OS" = "linux" ]; then
        run_root env PULSE_SPOOL_DIR="$VALIDATE_SPOOL" PULSE_SPOOL_MAX_BYTES=67108864 \
            "$BIN_PATH" --config "$CONFIG_PATH" "$@"
    else
        PULSE_SPOOL_DIR="$VALIDATE_SPOOL" PULSE_SPOOL_MAX_BYTES=67108864 \
            "$BIN_PATH" --config "$CONFIG_PATH" "$@"
    fi
}

confirm() {
    [ "$ASSUME_YES" = "1" ] && return 0
    [ -r /dev/tty ] || die "interactive input unavailable; use --unattended"
    printf '%s [Y/n] ' "$1" > /dev/tty
    IFS= read -r reply < /dev/tty || reply=""
    case "$reply" in
        ""|y|Y|yes|YES) return 0 ;;
        *) return 1 ;;
    esac
}

prompt_value() {
    prompt=$1
    default_value=$2
    [ -r /dev/tty ] || die "interactive input unavailable; use --unattended"
    if [ -n "$default_value" ]; then
        printf '%s [%s]: ' "$prompt" "$default_value" > /dev/tty
    else
        printf '%s: ' "$prompt" > /dev/tty
    fi
    IFS= read -r reply < /dev/tty || reply=""
    if [ -n "$reply" ]; then
        printf '%s' "$reply"
    else
        printf '%s' "$default_value"
    fi
}

prompt_secret() {
    [ -r /dev/tty ] || die "interactive input unavailable; use --unattended"
    printf 'Pulse ingest token: ' > /dev/tty
    old_stty=$(stty -g < /dev/tty)
    trap 'stty "$old_stty" < /dev/tty 2>/dev/null || true; exit 130' HUP INT TERM
    stty -echo < /dev/tty
    IFS= read -r secret < /dev/tty || secret=""
    stty "$old_stty" < /dev/tty
    trap cleanup HUP INT TERM
    printf '\n' > /dev/tty
    printf '%s' "$secret"
}

validate_single_line() {
    name=$1
    value=$2
    case "$value" in
        *'
'*) die "$name must be a single line" ;;
    esac
}

toml_escape() {
    validate_single_line "configuration value" "$1"
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

write_dimensions() {
    raw=$1
    [ -n "$raw" ] || return 0
    old_ifs=$IFS
    IFS=,
    for pair in $raw; do
        key=${pair%%=*}
        value=${pair#*=}
        [ "$key" != "$pair" ] || die "invalid dimension '$pair'; expected key=value"
        printf '%s\n' "$key" | grep -Eq '^[A-Za-z0-9_.-]+$' ||
            die "invalid dimension key: $key"
        printf '"%s" = "%s"\n' "$(toml_escape "$key")" "$(toml_escape "$value")"
    done
    IFS=$old_ifs
}

resolve_scheduler() {
    if [ "$SCHEDULER" != "auto" ]; then
        return
    fi
    if [ "$OS" = "darwin" ] &&
        [ -f "${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist" ]; then
        SCHEDULER=launchd
    elif [ "$OS" = "linux" ] && { [ -f "${SYSTEMD_DIR}/${SERVICE_ID}.service" ] ||
        [ -f "${SYSTEMD_DIR}/${SERVICE_ID}.timer" ]; }; then
        SCHEDULER=systemd
    elif [ "$OS" = "linux" ] && [ -f "${CRON_DIR}/${SERVICE_ID}" ]; then
        SCHEDULER=cron
    elif [ "$OS" = "darwin" ]; then
        SCHEDULER=launchd
    elif have systemctl; then
        SCHEDULER=systemd
    elif have crontab || [ -d /etc/cron.d ]; then
        SCHEDULER=cron
    else
        SCHEDULER=none
    fi
}

resolve_scheduler
case "$SCHEDULER" in
    none) ;;
    launchd) [ "$OS" = "darwin" ] || die "launchd is only supported on macOS" ;;
    systemd|cron) [ "$OS" = "linux" ] || die "$SCHEDULER is only supported on Linux" ;;
    *) die "unknown scheduler: $SCHEDULER" ;;
esac

remove_scheduler() {
    if [ "$OS" = "darwin" ]; then
        plist="${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"
        launchctl bootout "gui/$(id -u)" "$plist" >/dev/null 2>&1 || true
        rm -f "$plist"
    else
        if have systemctl; then
            run_root systemctl disable --now "${SERVICE_ID}.timer" >/dev/null 2>&1 || true
            run_root systemctl disable --now "${SERVICE_ID}.service" >/dev/null 2>&1 || true
        fi
        remove_root_file "${SYSTEMD_DIR}/${SERVICE_ID}.timer"
        remove_root_file "${SYSTEMD_DIR}/${SERVICE_ID}.service"
        remove_root_file "${CRON_DIR}/${SERVICE_ID}"
        if have systemctl; then
            run_root systemctl daemon-reload >/dev/null 2>&1 || true
        fi
    fi
}

if [ "$UNINSTALL" = "1" ]; then
    remove_scheduler
    remove_root_file "$BIN_PATH"
    log "removed $BINARY and its managed scheduler"
    log "preserved configuration at $CONFIG_PATH and the token next to it"
    exit 0
fi

have curl || die "curl is required"
have tar || die "tar is required"
have "$COSIGN" || die "cosign is required to authenticate Pulse releases"

TMP=$(mktemp -d)
VALIDATE_SPOOL="${TMP}/validate-spool"
cleanup() {
    if [ "$OS" = "linux" ] && [ -d "$VALIDATE_SPOOL" ]; then
        run_root rm -rf "$VALIDATE_SPOOL" 2>/dev/null || true
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"
if [ -n "${PULSE_RELEASE_BASE:-}" ]; then
    DOWNLOAD_BASE=${PULSE_RELEASE_BASE%/}
    RELEASE_TAG=${PULSE_RELEASE_TAG:-v0.0.0-test}
    curl -fsSL "${DOWNLOAD_BASE}/${ARCHIVE}" -o "${TMP}/${ARCHIVE}" ||
        die "could not download ${ARCHIVE}"
else
    if [ "$VERSION" = "latest" ]; then
        release_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "${GITHUB_BASE}/releases/latest") ||
            die "could not resolve the latest release"
        RELEASE_TAG=${release_url##*/}
    else
        case "$VERSION" in
            v*) RELEASE_TAG=$VERSION ;;
            *) RELEASE_TAG="v${VERSION}" ;;
        esac
    fi
    DOWNLOAD_BASE="${GITHUB_BASE}/releases/download/${RELEASE_TAG}"
    curl -fsSL "${DOWNLOAD_BASE}/${ARCHIVE}" -o "${TMP}/${ARCHIVE}" ||
        die "could not download ${ARCHIVE}"
fi

printf '%s\n' "$RELEASE_TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z][0-9A-Za-z.-]*)?$' ||
    die "invalid release tag: $RELEASE_TAG"

curl -fsSL "${DOWNLOAD_BASE}/checksums.txt" -o "${TMP}/checksums.txt" ||
    die "missing checksums.txt"
curl -fsSL "${DOWNLOAD_BASE}/checksums.txt.sig" -o "${TMP}/checksums.txt.sig" ||
    die "missing checksums.txt.sig"
curl -fsSL "${DOWNLOAD_BASE}/checksums.txt.pem" -o "${TMP}/checksums.txt.pem" ||
    die "missing checksums.txt.pem"

identity_tag=$(printf '%s' "$RELEASE_TAG" | sed 's/\./\\./g')
identity_regex="${PULSE_COSIGN_IDENTITY_REGEX:-^https://github\\.com/k2b-dev/pulse-injestors/\\.github/workflows/release\\.yml@refs/tags/${identity_tag}$}"
"$COSIGN" verify-blob \
    --certificate "${TMP}/checksums.txt.pem" \
    --signature "${TMP}/checksums.txt.sig" \
    --certificate-identity-regexp "$identity_regex" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    "${TMP}/checksums.txt" >/dev/null 2>&1 ||
    die "cosign verification failed"

expected=$(awk -v file="$ARCHIVE" '$2 == file || $2 == "*"file {print $1}' "${TMP}/checksums.txt")
[ -n "$expected" ] || die "$ARCHIVE is not listed in checksums.txt"
if have sha256sum; then
    actual=$(sha256sum "${TMP}/${ARCHIVE}" | awk '{print $1}')
elif have shasum; then
    actual=$(shasum -a 256 "${TMP}/${ARCHIVE}" | awk '{print $1}')
else
    die "sha256sum or shasum is required"
fi
[ "$actual" = "$expected" ] || die "checksum mismatch for $ARCHIVE"

tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"
[ -f "${TMP}/${BINARY}" ] || die "$BINARY is missing from the release archive"

CONFIG_EXISTS=0
WRITE_CONFIG=0
INTERVAL=${PULSE_INTERVAL_SECONDS:-$DEFAULT_INTERVAL}
if [ -f "$CONFIG_PATH" ]; then
    CONFIG_EXISTS=1
fi

if [ "$CONFIG_EXISTS" = "1" ] && [ "$RECONFIGURE" != "1" ]; then
    if [ -n "$CONFIG_SOURCE" ] && ! cmp -s "$CONFIG_SOURCE" "$CONFIG_PATH"; then
        die "existing config differs from --config-source; pass --reconfigure to replace it"
    fi
    log "preserving existing configuration at $CONFIG_PATH"
else
    WRITE_CONFIG=1
    staged_config="${TMP}/ingestor.toml"
    if [ -n "$CONFIG_SOURCE" ]; then
        [ -f "$CONFIG_SOURCE" ] || die "config source does not exist: $CONFIG_SOURCE"
        cp "$CONFIG_SOURCE" "$staged_config"
    else
        INGEST_URL=${PULSE_INGEST_URL:-}
        INGEST_TOKEN=${PULSE_INGEST_TOKEN:-}
        RESOURCE_ID=${PULSE_RESOURCE_ID:-}
        RESOURCE_LABEL=${PULSE_RESOURCE_LABEL:-}
        DIMENSIONS=${PULSE_DIMENSIONS:-}

        if [ -n "${PULSE_INGEST_TOKEN_FILE:-}" ]; then
            [ -f "$PULSE_INGEST_TOKEN_FILE" ] ||
                die "token file does not exist: $PULSE_INGEST_TOKEN_FILE"
            INGEST_TOKEN=$(cat "$PULSE_INGEST_TOKEN_FILE")
        fi

        if [ "$UNATTENDED" != "1" ]; then
            INGEST_URL=$(prompt_value "Pulse ingest URL" "$INGEST_URL")
            if [ -z "$INGEST_TOKEN" ]; then
                INGEST_TOKEN=$(prompt_secret)
            fi
            hostname_default=$(hostname 2>/dev/null || true)
            RESOURCE_ID=$(prompt_value "Stable resource id" "${RESOURCE_ID:-$hostname_default}")
            RESOURCE_LABEL=$(prompt_value "Resource label" "${RESOURCE_LABEL:-$RESOURCE_ID}")
            INTERVAL=$(prompt_value "Collection interval in seconds" "$INTERVAL")
            DIMENSIONS=$(prompt_value "Global dimensions (key=value,...; optional)" "$DIMENSIONS")
        fi

        [ -n "$INGEST_URL" ] || die "PULSE_INGEST_URL is required"
        case "$INGEST_URL" in
            http://*|https://*) ;;
            *) die "Pulse ingest URL must start with http:// or https://" ;;
        esac
        [ -n "$INGEST_TOKEN" ] || die "a Pulse ingest token or token file is required"
        [ -n "$RESOURCE_ID" ] || die "PULSE_RESOURCE_ID is required"
        [ -n "$RESOURCE_LABEL" ] || RESOURCE_LABEL=$RESOURCE_ID
        printf '%s\n' "$INTERVAL" | grep -Eq '^[1-9][0-9]*$' ||
            die "collection interval must be a positive integer"

        if [ "$OS" = "darwin" ]; then
            SPOOL_DIR=${PULSE_SPOOL_DIR:-"$HOME/Library/Application Support/Pulse/${BINARY}-spool"}
        else
            # systemd creates this as StateDirectory=pulse/<binary>.
            SPOOL_DIR=${PULSE_SPOOL_DIR:-"/var/lib/pulse/${BINARY}"}
        fi
        SPOOL_MAX_BYTES=${PULSE_SPOOL_MAX_BYTES:-67108864}
        printf '%s\n' "$SPOOL_MAX_BYTES" | grep -Eq '^[1-9][0-9]*$' ||
            die "spool byte budget must be a positive integer"

        {
            printf '[pulse]\n'
            printf 'ingest_url = "%s"\n' "$(toml_escape "$INGEST_URL")"
            printf 'ingest_token_file = "%s"\n' "$(toml_escape "$TOKEN_PATH")"
            printf 'spool_dir = "%s"\n' "$(toml_escape "$SPOOL_DIR")"
            printf 'spool_max_bytes = %s\n\n' "$SPOOL_MAX_BYTES"
            printf '[resource]\n'
            printf 'id = "%s"\n' "$(toml_escape "$RESOURCE_ID")"
            printf 'label = "%s"\n' "$(toml_escape "$RESOURCE_LABEL")"
            printf '\n[dimensions]\n'
            write_dimensions "$DIMENSIONS"
            printf '\n[runner]\n'
            printf 'interval_seconds = %s\n' "$INTERVAL"
            printf 'collector_timeout_seconds = 60\n'
            printf '\n[http]\n'
            printf 'timeout_seconds = 10\n'
            printf 'max_retries = 3\n'
            printf 'initial_backoff_ms = 500\n'
        } > "$staged_config"
        staged_token="${TMP}/ingestor.token"
        validate_single_line "Pulse ingest token" "$INGEST_TOKEN"
        (umask 077 && printf '%s\n' "$INGEST_TOKEN" > "$staged_token")
    fi
fi

printf '%s\n' "$INTERVAL" | grep -Eq '^[1-9][0-9]*$' ||
    die "collection interval must be a positive integer"

log "release: $RELEASE_TAG"
log "ingestor: $INGESTOR"
log "binary: $BIN_PATH"
log "config: $CONFIG_PATH"
log "scheduler: $SCHEDULER"
confirm "Install and enroll this host?" || die "cancelled"

install_root_file "${TMP}/${BINARY}" "$BIN_PATH" 0755
"$BIN_PATH" --version

if [ "$WRITE_CONFIG" = "1" ]; then
    install_config_file "$staged_config"
    log "wrote configuration with mode 0600"
    if [ -n "${staged_token:-}" ]; then
        install_token_file "$staged_token"
        log "wrote token file $TOKEN_PATH with mode 0600"
    fi
fi

log "validating local collection"
run_ingestor --local once >/dev/null
log "sending first Pulse batch"
run_ingestor once

install_systemd() {
    service_file="${TMP}/${SERVICE_ID}.service"
    # Root collectors read hardware and package state; uptime only needs the network.
    if [ "$INGESTOR" = "uptime" ]; then
        identity="DynamicUser=yes
ProtectSystem=strict
ProtectHome=yes"
    else
        identity="ProtectSystem=full
ProtectHome=read-only"
    fi
    credential=""
    if token_file_exists; then
        credential="LoadCredential=${CREDENTIAL_NAME}:${TOKEN_PATH}
Environment=PULSE_INGEST_TOKEN_FILE=%d/${CREDENTIAL_NAME}"
    fi
    cat > "$service_file" <<EOF
[Unit]
Description=Pulse ${INGESTOR} telemetry
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=0

[Service]
Type=notify
ExecStart="${BIN_PATH}" --config %d/pulse-config run
Restart=always
RestartSec=10s
WatchdogSec=120s
TimeoutStopSec=30s
LoadCredential=pulse-config:${CONFIG_PATH}
${credential}
StateDirectory=pulse/${BINARY}
StateDirectoryMode=0700
${identity}
UMask=0077
Nice=10
IOSchedulingClass=idle
MemoryMax=256M
TasksMax=64
NoNewPrivileges=yes
PrivateTmp=yes
ProtectKernelModules=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
ProtectClock=yes
RestrictSUIDSGID=yes
RestrictRealtime=yes
LockPersonality=yes
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
EOF
    install_root_file "$service_file" "${SYSTEMD_DIR}/${SERVICE_ID}.service" 0644
    run_root systemctl disable --now "${SERVICE_ID}.timer" >/dev/null 2>&1 || true
    remove_root_file "${SYSTEMD_DIR}/${SERVICE_ID}.timer"
    remove_root_file "${CRON_DIR}/${SERVICE_ID}"
    run_root systemctl daemon-reload
    run_root systemctl enable "${SERVICE_ID}.service"
    run_root systemctl restart "${SERVICE_ID}.service"
}

cron_schedule() {
    seconds=$1
    [ $((seconds % 60)) -eq 0 ] ||
        die "cron requires an interval divisible by 60 seconds"
    minutes=$((seconds / 60))
    if [ "$minutes" -le 59 ]; then
        printf '*/%s * * * *' "$minutes"
    elif [ $((minutes % 60)) -eq 0 ] && [ $((minutes / 60)) -le 23 ]; then
        printf '0 */%s * * *' $((minutes / 60))
    elif [ "$minutes" -eq 1440 ]; then
        printf '0 0 * * *'
    else
        die "cron interval is not representable; use systemd or a different interval"
    fi
}

install_cron() {
    case "${BIN_PATH}${CONFIG_PATH}" in
        *[[:space:]]*) die "cron does not support whitespace in managed binary or config paths" ;;
    esac
    cron_file="${TMP}/${SERVICE_ID}"
    schedule=$(cron_schedule "$INTERVAL")
    cat > "$cron_file" <<EOF
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
${schedule} root ${BIN_PATH} --config ${CONFIG_PATH} once
EOF
    install_root_file "$cron_file" "${CRON_DIR}/${SERVICE_ID}" 0644
    if have systemctl; then
        run_root systemctl disable --now "${SERVICE_ID}.timer" >/dev/null 2>&1 || true
        run_root systemctl disable --now "${SERVICE_ID}.service" >/dev/null 2>&1 || true
    fi
    remove_root_file "${SYSTEMD_DIR}/${SERVICE_ID}.timer"
    remove_root_file "${SYSTEMD_DIR}/${SERVICE_ID}.service"
}

xml_escape() {
    printf '%s' "$1" |
        sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g'
}

install_launchd() {
    [ "$(id -u)" -ne 0 ] ||
        die "run the macOS installer as the target user, without sudo"
    agents_dir="${HOME}/Library/LaunchAgents"
    logs_dir="${HOME}/Library/Logs/Pulse"
    plist="${agents_dir}/${PLIST_LABEL}.plist"
    staged_plist="${TMP}/${PLIST_LABEL}.plist"
    install -d -m 0755 "$agents_dir"
    install -d -m 0755 "$logs_dir"
    cat > "$staged_plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${PLIST_LABEL}</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(xml_escape "$BIN_PATH")</string>
    <string>--config</string>
    <string>$(xml_escape "$CONFIG_PATH")</string>
    <string>run</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>ProcessType</key>
  <string>Background</string>
  <key>LowPriorityIO</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$(xml_escape "${logs_dir}/${BINARY}.out.log")</string>
  <key>StandardErrorPath</key>
  <string>$(xml_escape "${logs_dir}/${BINARY}.err.log")</string>
</dict>
</plist>
EOF
    plutil -lint "$staged_plist" >/dev/null
    install -m 0644 "$staged_plist" "$plist"
    launchctl bootout "gui/$(id -u)" "$plist" >/dev/null 2>&1 || true
    launchctl bootstrap "gui/$(id -u)" "$plist"
    launchctl enable "gui/$(id -u)/${PLIST_LABEL}"
}

case "$SCHEDULER" in
    systemd) install_systemd ;;
    cron) install_cron ;;
    launchd) install_launchd ;;
    none) log "scheduler disabled" ;;
esac

log "enrollment complete"
log "binary: $BIN_PATH"
log "config: $CONFIG_PATH"
case "$SCHEDULER" in
    systemd)
        log "status: systemctl status ${SERVICE_ID}.service"
        log "logs: journalctl -u ${SERVICE_ID}.service"
        ;;
    cron) log "schedule: ${CRON_DIR}/${SERVICE_ID}" ;;
    launchd)
        log "status: launchctl print gui/$(id -u)/${PLIST_LABEL}"
        log "logs: ${HOME}/Library/Logs/Pulse"
        ;;
esac
