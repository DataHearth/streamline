#!/usr/bin/env bash
_cs_boot="${COMMUNITY_SCRIPTS_CORE_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../core}/core/build.func"
source "$_cs_boot" 2>/dev/null || source <(curl -fsSL "${COMMUNITY_SCRIPTS_CORE_URL:-https://raw.githubusercontent.com/community-scripts/core/main}/core/build.func")
# Copyright (c) 2021-2026 community-scripts ORG
# Author: DataHearth
# License: MIT | https://github.com/community-scripts/ProxmoxVE/raw/main/LICENSE
# Source: https://github.com/DataHearth/streamline

APP="Streamline"
var_tags="${var_tags:-arr;media}"
var_cpu="${var_cpu:-2}"
var_ram="${var_ram:-2048}"
var_disk="${var_disk:-8}"
var_os="${var_os:-debian}"
var_version="${var_version:-13}"
var_arm64="${var_arm64:-yes}"
var_unprivileged="${var_unprivileged:-1}"
var_gpu="${var_gpu:-yes}"

header_info "$APP"
variables
color
catch_errors

function update_script() {
  header_info
  check_container_storage
  check_container_resources
  if [[ ! -f /usr/local/bin/streamline ]]; then
    msg_error "No ${APP} Installation Found!"
    exit
  fi

  if check_for_gh_release "streamline" "DataHearth/streamline"; then
    RELEASE="${CHECK_UPDATE_RELEASE#v}"
    ARCHIVE="streamline_${RELEASE}_linux_$(arch_resolve).tar.gz"
    TMP_DIR=$(mktemp -d)

    # Fetched and checked before the service stops, so a failed download
    # leaves the running version alone.
    msg_info "Downloading Streamline ${RELEASE}"
    curl -fsSL -o "${TMP_DIR}/${ARCHIVE}" "https://github.com/DataHearth/streamline/releases/download/v${RELEASE}/${ARCHIVE}"
    curl -fsSL -o "${TMP_DIR}/checksums.txt" "https://github.com/DataHearth/streamline/releases/download/v${RELEASE}/checksums.txt"
    awk -v f="$ARCHIVE" '$2 == f' "${TMP_DIR}/checksums.txt" >"${TMP_DIR}/archive.sha256"
    if [[ ! -s "${TMP_DIR}/archive.sha256" ]] || ! (cd "$TMP_DIR" && sha256sum --check --quiet archive.sha256); then
      msg_error "Checksum verification failed for ${ARCHIVE}"
      rm -rf "$TMP_DIR"
      exit 1
    fi
    tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR" --no-same-owner streamline
    msg_ok "Downloaded Streamline ${RELEASE}"

    msg_info "Stopping Service"
    systemctl stop streamline
    msg_ok "Stopped Service"

    # The new version migrates the database at boot, and a migration only
    # goes forward: this copy is the way back.
    msg_info "Backing up Database"
    DATA_DIR=$(awk '/^data_dir:/ {print $2}' /etc/streamline/config.yaml | tr -d "\"'")
    DATA_DIR="${DATA_DIR:-/var/lib/streamline}"
    [[ "$DATA_DIR" == /* ]] || DATA_DIR="/var/lib/streamline/${DATA_DIR#./}"
    if [[ -f "${DATA_DIR}/streamline.db" ]]; then
      BACKUP="${DATA_DIR}/backups/streamline-$(cat ~/.streamline 2>/dev/null || echo unknown)-$(date -u +%Y%m%dT%H%M%SZ).db"
      install -d -m 0750 -o streamline -g streamline "${DATA_DIR}/backups"
      for suffix in "" -wal -shm; do
        if [[ -f "${DATA_DIR}/streamline.db${suffix}" ]]; then
          cp -p "${DATA_DIR}/streamline.db${suffix}" "${BACKUP}${suffix}"
        fi
      done
      msg_ok "Backed up Database to ${BACKUP}"
    else
      msg_ok "No Database to back up"
    fi

    msg_info "Updating Streamline"
    install -m 0755 "${TMP_DIR}/streamline" /usr/local/bin/streamline.new
    mv -f /usr/local/bin/streamline.new /usr/local/bin/streamline
    rm -rf "$TMP_DIR"
    echo "${RELEASE}" >~/.streamline
    msg_ok "Updated Streamline"

    msg_info "Starting Service"
    systemctl start streamline
    msg_ok "Started Service"
    msg_ok "Updated successfully!"
  fi
  exit
}

start
build_container
description

msg_ok "Completed successfully!\n"
echo -e "${CREATING}${GN}${APP} setup has been successfully initialized!${CL}"
echo -e "${INFO}${YW} Access it using the following URL:${CL}"
echo -e "${GATEWAY}${BGN}http://${IP}:8080${CL}"
echo -e "${INFO}${YW} Admin password (shown once): journalctl -u streamline | grep 'default admin'${CL}"
