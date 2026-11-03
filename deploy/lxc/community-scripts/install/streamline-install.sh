#!/usr/bin/env bash

# Copyright (c) 2021-2026 community-scripts ORG
# Author: DataHearth
# License: MIT | https://github.com/community-scripts/ProxmoxVE/raw/main/LICENSE
# Source: https://github.com/DataHearth/streamline

source /dev/stdin <<<"$FUNCTIONS_FILE_PATH"
color
verb_ip6
catch_errors
setting_up_container
network_check
update_os

msg_info "Installing Dependencies"
$STD apt install -y ffmpeg
msg_ok "Installed Dependencies"

# Not fetch_and_deploy_gh_release: it does not check the archive against the
# release's checksums.txt, and Streamline publishes one (cosign-signed).
msg_info "Installing Streamline"
RELEASE=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/DataHearth/streamline/releases/latest)
RELEASE="${RELEASE##*/v}"
ARCHIVE="streamline_${RELEASE}_linux_$(arch_resolve).tar.gz"
TMP_DIR=$(mktemp -d)
curl -fsSL -o "${TMP_DIR}/${ARCHIVE}" "https://github.com/DataHearth/streamline/releases/download/v${RELEASE}/${ARCHIVE}"
curl -fsSL -o "${TMP_DIR}/checksums.txt" "https://github.com/DataHearth/streamline/releases/download/v${RELEASE}/checksums.txt"
awk -v f="$ARCHIVE" '$2 == f' "${TMP_DIR}/checksums.txt" >"${TMP_DIR}/archive.sha256"
if [[ ! -s "${TMP_DIR}/archive.sha256" ]] || ! (cd "$TMP_DIR" && sha256sum --check --quiet archive.sha256); then
  msg_error "Checksum verification failed for ${ARCHIVE}"
  exit 1
fi
tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR" --no-same-owner streamline
install -m 0755 "${TMP_DIR}/streamline" /usr/local/bin/streamline
rm -rf "$TMP_DIR"
echo "${RELEASE}" >~/.streamline
msg_ok "Installed Streamline ${RELEASE}"

msg_info "Configuring Streamline"
useradd --system --home-dir /var/lib/streamline --shell /usr/sbin/nologin --user-group streamline
install -d -m 0750 -o streamline -g streamline /var/lib/streamline /etc/streamline
$STD /usr/local/bin/streamline config init --output /etc/streamline/config.yaml
sed -i 's|^data_dir: .*|data_dir: /var/lib/streamline|' /etc/streamline/config.yaml
chown streamline:streamline /etc/streamline/config.yaml
chmod 0600 /etc/streamline/config.yaml
msg_ok "Configured Streamline"

setup_hwaccel "streamline"

msg_info "Creating Service"
cat <<EOF2 >/etc/systemd/system/streamline.service
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
UMask=0002
NoNewPrivileges=true
ProtectSystem=full
ReadWritePaths=/etc/streamline /var/lib/streamline
ProtectHome=true
PrivateTmp=true
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
EOF2
systemctl enable -q --now streamline
msg_ok "Created Service"

motd_ssh
customize
cleanup_lxc
