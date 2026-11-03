#!/usr/bin/env bash
# Install or update Streamline inside a Debian or Ubuntu LXC container (or any
# systemd host). Run it as root inside the container:
#
#   curl -fsSL https://raw.githubusercontent.com/DataHearth/streamline/main/deploy/lxc/streamline.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/DataHearth/streamline/main/deploy/lxc/streamline.sh | bash -s -- update
#
# Install also leaves `streamline-update` on the PATH, which runs the second.
#
# Environment:
#   STREAMLINE_VERSION           release to install, e.g. 3.2.0 (default: latest)
#   STREAMLINE_VERIFY_SIGNATURE  auto (default: verify checksums.txt with cosign
#                                when it is installed), yes (require it), no
#
# What install does, and the update path, are in docs/wiki/Proxmox.md.
# deploy/lxc/community-scripts/ holds the same install in the Proxmox VE
# Helper-Scripts layout; a change to one belongs in the other.
set -Eeuo pipefail

REPO="DataHearth/streamline"
VERSION="${STREAMLINE_VERSION:-latest}"
VERIFY_SIGNATURE="${STREAMLINE_VERIFY_SIGNATURE:-auto}"
SCRIPT_URL="https://raw.githubusercontent.com/${REPO}/main/deploy/lxc/streamline.sh"

BIN=/usr/local/bin/streamline
CONFIG=/etc/streamline/config.yaml
DATA_DIR=/var/lib/streamline
UNIT=/etc/systemd/system/streamline.service
VERSION_FILE=/var/lib/streamline/.installed-version

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() {
  printf '\033[1;31merror:\033[0m %s\n' "$*" >&2
  exit 1
}

arch() {
  case "$(uname -m)" in
  x86_64 | amd64) echo amd64 ;;
  aarch64 | arm64) echo arm64 ;;
  *) die "unsupported architecture $(uname -m): releases are built for amd64 and arm64" ;;
  esac
}

# resolve_version prints the release to install without a leading v. The
# latest one is read off the releases/latest redirect rather than the API, so an
# unauthenticated host is never rate-limited out of an update.
resolve_version() {
  if [[ $VERSION != latest ]]; then
    echo "${VERSION#v}"
    return
  fi
  local url
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") ||
    die "could not reach GitHub to find the latest release"
  [[ ${url##*/} == v* ]] || die "could not resolve the latest release from ${url}"
  echo "${url##*/v}"
}

# fetch_verified downloads the release archive for this architecture into $2 and
# checks it against checksums.txt, and checksums.txt against its cosign bundle.
fetch_verified() {
  local version=$1 dir=$2
  local base="https://github.com/${REPO}/releases/download/v${version}"
  local archive="streamline_${version}_linux_${ARCH}.tar.gz"

  info "Downloading Streamline ${version} (${ARCH})"
  curl -fsSL --retry 3 -o "${dir}/${archive}" "${base}/${archive}" ||
    die "download failed: ${base}/${archive}"
  curl -fsSL --retry 3 -o "${dir}/checksums.txt" "${base}/checksums.txt" ||
    die "download failed: ${base}/checksums.txt"

  case "$VERIFY_SIGNATURE" in
  no) ;;
  yes | auto)
    if command -v cosign >/dev/null; then
      curl -fsSL --retry 3 -o "${dir}/checksums.txt.bundle" "${base}/checksums.txt.bundle" ||
        die "download failed: ${base}/checksums.txt.bundle"
      # The identity is case-sensitive and GitHub writes the owner as DataHearth.
      cosign verify-blob "${dir}/checksums.txt" \
        --bundle "${dir}/checksums.txt.bundle" \
        --certificate-identity-regexp "^https://github.com/${REPO}/.github/workflows/release.yaml@refs/tags/v" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com >/dev/null 2>&1 ||
        die "checksums.txt does not carry a valid signature from ${REPO}'s release workflow"
      info "Verified the signature on checksums.txt"
    elif [[ $VERIFY_SIGNATURE == yes ]]; then
      die "STREAMLINE_VERIFY_SIGNATURE=yes but cosign is not installed"
    fi
    ;;
  *) die "STREAMLINE_VERIFY_SIGNATURE must be auto, yes or no, not ${VERIFY_SIGNATURE}" ;;
  esac

  awk -v f="$archive" '$2 == f' "${dir}/checksums.txt" >"${dir}/archive.sha256"
  [[ -s ${dir}/archive.sha256 ]] || die "${archive} is not listed in checksums.txt"
  (cd "$dir" && sha256sum --check --quiet archive.sha256) || die "checksum mismatch for ${archive}"
  info "Verified the checksum of ${archive}"

  tar -xzf "${dir}/${archive}" -C "$dir" --no-same-owner streamline
}

# place_binary installs the binary fetch_verified unpacked into $1. Beside the
# old one, then renamed over it: nothing ever executes a half-written file.
place_binary() {
  install -m 0755 "${1}/streamline" "${BIN}.new"
  mv -f "${BIN}.new" "$BIN"
}

# backup_database copies the database aside while the service is stopped, so an
# update whose migration goes wrong can be undone. The five newest are kept.
backup_database() {
  local from=$1 data_dir db stamp dest
  data_dir=$(awk '/^data_dir:/ {print $2}' "$CONFIG" 2>/dev/null | tr -d "\"'")
  data_dir=${data_dir:-$DATA_DIR}
  # A relative data_dir resolves against the unit's WorkingDirectory.
  [[ $data_dir == /* ]] || data_dir="${DATA_DIR}/${data_dir#./}"
  db="${data_dir}/streamline.db"
  [[ -f $db ]] || return 0

  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  dest="${data_dir}/backups/streamline-${from:-unknown}-${stamp}.db"
  install -d -m 0750 -o streamline -g streamline "${data_dir}/backups"
  local suffix
  for suffix in "" -wal -shm; do
    [[ -f ${db}${suffix} ]] && cp -p "${db}${suffix}" "${dest}${suffix}"
  done
  info "Backed up the database to ${dest}"
  # Oldest first; everything but the five newest .db files and their -wal/-shm.
  find "${data_dir}/backups" -maxdepth 1 -name 'streamline-*.db' -printf '%T@ %p\n' |
    sort -n | head -n -5 | cut -d' ' -f2- | while read -r old; do
    rm -f "$old" "${old}-wal" "${old}-shm"
  done
}

write_unit() {
  cat >"$UNIT" <<'EOF'
[Unit]
Description=Streamline media manager
Documentation=https://github.com/DataHearth/streamline/wiki
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=streamline
Group=streamline
WorkingDirectory=/var/lib/streamline
ExecStart=/usr/local/bin/streamline --config /etc/streamline/config.yaml
Restart=on-failure
RestartSec=5
# Group-writable, so a media group shared with other containers can manage
# what Streamline imports.
UMask=0002

# Hardening. ProtectSystem=full rather than strict: strict would need every
# library and download mount listed here, and a mount added later would fail
# imports with a read-only error nowhere near this file. /etc stays read-only
# except the config, which the web UI writes back to.
NoNewPrivileges=true
ProtectSystem=full
ReadWritePaths=/etc/streamline /var/lib/streamline
ProtectHome=true
PrivateTmp=true
# Not PrivateDevices: it would hide /dev/dri from the VAAPI transcoder.
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictSUIDSGID=true
RestrictRealtime=true
RestrictNamespaces=true
LockPersonality=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
EOF
}

do_install() {
  [[ -x $BIN ]] && die "Streamline is already installed; run with 'update' to upgrade it"

  info "Installing dependencies"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends ca-certificates curl ffmpeg >/dev/null

  local version
  version=$(resolve_version)
  fetch_verified "$version" "$WORK"
  place_binary "$WORK"

  info "Creating the streamline user"
  if ! id streamline >/dev/null 2>&1; then
    useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin --user-group streamline
  fi
  # VAAPI: the render node is root:render (or video) 0660. Joining both is
  # harmless where the device is not passed through.
  local group
  for group in render video; do
    if getent group "$group" >/dev/null; then
      usermod -aG "$group" streamline
    fi
  done
  install -d -m 0750 -o streamline -g streamline "$DATA_DIR" /etc/streamline

  if [[ ! -f $CONFIG ]]; then
    info "Writing ${CONFIG}"
    "$BIN" config init --output "$CONFIG" >/dev/null 2>&1
    sed -i "s|^data_dir: .*|data_dir: ${DATA_DIR}|" "$CONFIG"
    grep -q "^data_dir: ${DATA_DIR}\$" "$CONFIG" || die "could not set data_dir in ${CONFIG}"
    # Owner-only whatever the release's `config init` did: the file goes on
    # to hold the session secret and every integration's credentials.
    chown streamline:streamline "$CONFIG"
    chmod 0600 "$CONFIG"
  fi
  echo "$version" >"$VERSION_FILE"

  cat >/usr/local/sbin/streamline-update <<EOF
#!/usr/bin/env bash
# Updates Streamline to the latest release. Written by ${SCRIPT_URL}
set -euo pipefail
curl -fsSL ${SCRIPT_URL} | bash -s -- update
EOF
  chmod 0755 /usr/local/sbin/streamline-update

  info "Starting the service"
  write_unit
  systemctl daemon-reload
  systemctl enable --now -q streamline

  local ip
  ip=$(hostname -I 2>/dev/null | awk '{print $1}')
  info "Streamline ${version} is running: http://${ip:-<container-ip>}:8080"
  echo "    The admin password is printed once on first start; read it from the journal:"
  echo "      journalctl -u streamline | grep 'default admin'"
  echo "    Update later with: streamline-update"
}

do_update() {
  [[ -x $BIN ]] || die "Streamline is not installed here; run without arguments to install it"

  local current="" version
  [[ -f $VERSION_FILE ]] && current=$(<"$VERSION_FILE")
  version=$(resolve_version)
  if [[ $current == "$version" ]]; then
    info "Streamline ${version} is already installed"
    return
  fi

  # Fetched and verified before the service stops, so a failed download or a
  # bad checksum leaves the running version alone.
  fetch_verified "$version" "$WORK"

  info "Updating Streamline ${current:-unknown} -> ${version}"
  systemctl stop streamline
  backup_database "$current"
  place_binary "$WORK"
  # The unit is ours: refresh it so hardening changes reach old installs.
  write_unit
  systemctl daemon-reload
  # The new version migrates the database at boot.
  systemctl start streamline
  echo "$version" >"$VERSION_FILE"
  info "Updated to ${version}. Logs: journalctl -u streamline -f"
}

main() {
  [[ $EUID -eq 0 ]] || die "run this as root"
  command -v systemctl >/dev/null || die "systemd is required"
  command -v curl >/dev/null || die "curl is required: apt-get install curl"
  ARCH=$(arch)
  WORK=$(mktemp -d)
  trap 'rm -rf "$WORK"' EXIT

  case "${1:-install}" in
  install) do_install ;;
  update) do_update ;;
  *) die "usage: $0 [install|update]" ;;
  esac
}

main "$@"
