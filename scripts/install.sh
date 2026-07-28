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

Install, configure, verify, and schedule one native Pulse ingestor.
Docker uses the separate Compose deployment and is not supported here.

  --ingestor=NAME       auto, linux, macos, proxmox, pbs, or uptime
  --scheduler=NAME      auto, systemd, cron, launchd, or none
  --config-source=PATH  install an existing complete TOML config
  --config-path=PATH    destination config path
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
  PULSE_ENTITY_ID
  PULSE_ENTITY_LABEL (optional; defaults to entity id)
  PULSE_INTERVAL_SECONDS (optional; platform default)
  PULSE_DIMENSIONS (optional comma-separated key=value pairs)

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
        CONFIG_PATH="${HOME}/Library/Application Support/Pulse/ingestor.toml"
    else
        CONFIG_PATH="/etc/pulse/ingestor.toml"
    fi
fi

BIN_PATH="${PREFIX}/${BINARY}"
SERVICE_ID="$BINARY"
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

run_ingestor() {
    if [ "$OS" = "linux" ]; then
        run_root "$BIN_PATH" --config "$CONFIG_PATH" "$@"
    else
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
    elif [ "$OS" = "linux" ] && [ -f "${SYSTEMD_DIR}/${SERVICE_ID}.timer" ]; then
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
    log "preserved configuration at $CONFIG_PATH"
    exit 0
fi

have curl || die "curl is required"
have tar || die "tar is required"
have "$COSIGN" || die "cosign is required to authenticate Pulse releases"

TMP=$(mktemp -d)
cleanup() {
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
        ENTITY_ID=${PULSE_ENTITY_ID:-}
        ENTITY_LABEL=${PULSE_ENTITY_LABEL:-}
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
            ENTITY_ID=$(prompt_value "Stable entity id" "${ENTITY_ID:-$hostname_default}")
            ENTITY_LABEL=$(prompt_value "Entity label" "${ENTITY_LABEL:-$ENTITY_ID}")
            INTERVAL=$(prompt_value "Collection interval in seconds" "$INTERVAL")
            DIMENSIONS=$(prompt_value "Global dimensions (key=value,...; optional)" "$DIMENSIONS")
        fi

        [ -n "$INGEST_URL" ] || die "PULSE_INGEST_URL is required"
        case "$INGEST_URL" in
            http://*|https://*) ;;
            *) die "Pulse ingest URL must start with http:// or https://" ;;
        esac
        [ -n "$INGEST_TOKEN" ] || die "a Pulse ingest token or token file is required"
        [ -n "$ENTITY_ID" ] || die "PULSE_ENTITY_ID is required"
        [ -n "$ENTITY_LABEL" ] || ENTITY_LABEL=$ENTITY_ID
        printf '%s\n' "$INTERVAL" | grep -Eq '^[1-9][0-9]*$' ||
            die "collection interval must be a positive integer"

        {
            printf '[pulse]\n'
            printf 'ingest_url = "%s"\n' "$(toml_escape "$INGEST_URL")"
            printf 'ingest_token = "%s"\n\n' "$(toml_escape "$INGEST_TOKEN")"
            printf '[entity]\n'
            printf 'id = "%s"\n' "$(toml_escape "$ENTITY_ID")"
            printf 'label = "%s"\n' "$(toml_escape "$ENTITY_LABEL")"
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
fi

log "validating local collection"
run_ingestor --local once >/dev/null
log "sending first Pulse batch"
run_ingestor once

install_systemd() {
    service_file="${TMP}/${SERVICE_ID}.service"
    timer_file="${TMP}/${SERVICE_ID}.timer"
    cat > "$service_file" <<EOF
[Unit]
Description=Pulse ${INGESTOR} telemetry collection
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
ExecStart="${BIN_PATH}" --config "${CONFIG_PATH}" once
Nice=10
IOSchedulingClass=idle
EOF
    cat > "$timer_file" <<EOF
[Unit]
Description=Collect Pulse ${INGESTOR} telemetry every ${INTERVAL} seconds

[Timer]
OnBootSec=2min
OnUnitActiveSec=${INTERVAL}s
RandomizedDelaySec=15s
Persistent=true
Unit=${SERVICE_ID}.service

[Install]
WantedBy=timers.target
EOF
    install_root_file "$service_file" "${SYSTEMD_DIR}/${SERVICE_ID}.service" 0644
    install_root_file "$timer_file" "${SYSTEMD_DIR}/${SERVICE_ID}.timer" 0644
    remove_root_file "${CRON_DIR}/${SERVICE_ID}"
    run_root systemctl daemon-reload
    run_root systemctl enable --now "${SERVICE_ID}.timer"
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
    <string>once</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>${INTERVAL}</integer>
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
    systemd) log "status: systemctl status ${SERVICE_ID}.timer" ;;
    cron) log "schedule: ${CRON_DIR}/${SERVICE_ID}" ;;
    launchd)
        log "status: launchctl print gui/$(id -u)/${PLIST_LABEL}"
        log "logs: ${HOME}/Library/Logs/Pulse"
        ;;
esac
