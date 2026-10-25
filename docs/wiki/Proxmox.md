# Proxmox VE (LXC)

Streamline is one static binary with release archives for linux/amd64 and arm64, which suits an LXC container well: no Docker inside it, no runtime, and `ffmpeg` from Debian.

- [1. Create the container](#1-create-the-container)
- [2. Install Streamline](#2-install-streamline)
- [3. Mount your media](#3-mount-your-media)
- [4. Share ownership in an unprivileged container](#4-share-ownership-in-an-unprivileged-container)
- [5. Hardware transcoding (VAAPI)](#5-hardware-transcoding-vaapi)
- [Updating](#updating)
- [What the script installs](#what-the-script-installs)
- [Proxmox VE Helper-Scripts](#proxmox-ve-helper-scripts)

---

## 1. Create the container

In the Proxmox UI: **Create CT** with a Debian 13 (or 12, or Ubuntu 24.04) template, **unprivileged**, with *Nesting* enabled (the default for new unprivileged containers; the service's systemd sandboxing needs it). Two cores, 2 GiB of memory and 8 GiB of disk are plenty — your media does not live on that disk.

Or from the host shell:

```bash
pct create 120 local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst \
  --hostname streamline --unprivileged 1 --features nesting=1 \
  --cores 2 --memory 2048 --rootfs local-lvm:8 \
  --net0 name=eth0,bridge=vmbr0,ip=dhcp --onboot 1
pct start 120
```

(`pveam available --section system` lists the templates you can download; use the name you actually have.)

## 2. Install Streamline

Inside the container, as root (`pct enter 120`):

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/DataHearth/streamline/main/deploy/lxc/streamline.sh | bash
```

It downloads the latest release for the container's architecture, checks it against the release's `checksums.txt` (and that file's cosign signature when `cosign` is installed), installs `ffmpeg`, creates a `streamline` user, writes `/etc/streamline/config.yaml` with `data_dir: /var/lib/streamline`, and starts a hardened systemd service on port **8080**.

The first start creates the admin account `admin@streamline.local` with a generated password, printed **once** to the service's output, which the journal keeps:

```bash
journalctl -u streamline | grep 'default admin'
```

It is never written to the config file. Lost it before logging in? Set a new one: `systemctl stop streamline && runuser -u streamline -- streamline -c /etc/streamline/config.yaml auth set-password admin@streamline.local --generate && systemctl start streamline` (as the `streamline` user, so the database files keep their owner).

Then continue with [First-Run Setup](First-Run-Setup). The library folders go in the config file (step 3 there), and must be the paths of the mount you add next.

| Variable | Effect |
| --- | --- |
| `STREAMLINE_VERSION=3.2.0` | Install that release instead of the latest |
| `STREAMLINE_VERIFY_SIGNATURE=yes` | Refuse to install unless `cosign` is present and the signature checks out (`no` skips it; the default, `auto`, checks when `cosign` is installed) |

Pass them to `bash`: `curl -fsSL … | STREAMLINE_VERSION=3.2.0 bash`.

## 3. Mount your media

Downloads and media must be **one** mount, for the reason in [the folder rule](Installation#before-you-start-the-folder-rule). Bind-mount the host directory holding both into the container as a mount point, from the host shell:

```bash
pct set 120 -mp0 /tank/data,mp=/mnt/data
pct reboot 120
```

Then in `/etc/streamline/config.yaml`:

```yaml
library:
  movie_path: /mnt/data/media/movies
  series_path: /mnt/data/media/series
  download_path: /mnt/data/downloads
```

and `systemctl restart streamline`. Your torrent client — in another container, or Streamline's built-in engine — must see the same folders at the same paths.

> [!NOTE]
> The service is sandboxed with `ProtectSystem=full` and `ProtectHome=true`: it can write anywhere except `/usr`, `/boot`, `/etc` (but for `/etc/streamline`) and `/home`. Mount your media under `/mnt`, `/srv` or `/media`, not `/home`.

## 4. Share ownership in an unprivileged container

An unprivileged container shifts every uid and gid by 100000: the `streamline` user (say uid 999 inside) is uid 100999 on the host, and a share owned by a host user shows up inside as `nobody:nogroup`, unwritable. Pick one:

**Give the share to the shifted ids.** Simplest when only this container writes there. On the host:

```bash
pct exec 120 -- id streamline        # e.g. uid=999(streamline) gid=999(streamline)
chown -R 100999:100999 /tank/data    # 100000 + the ids inside
```

**Share a group with other containers.** When Jellyfin, a torrent client and Streamline all write the same tree, give them one group and map it straight through. With a host group `media` of gid 1001, and `media` (gid 1001) created inside each container, add to `/etc/pve/lxc/120.conf`:

```
lxc.idmap: u 0 100000 65536
lxc.idmap: g 0 100000 1001
lxc.idmap: g 1001 1001 1
lxc.idmap: g 1002 101002 64534
```

allow it once in `/etc/subgid` on the host (`root:1001:1`), then inside the container `usermod -aG media streamline` and make the tree group-writable on the host (`chgrp -R media /tank/data && chmod -R g+rwX /tank/data`, plus `chmod g+s` on directories so new files keep the group). The service runs with `UMask=0002`, so what Streamline creates stays writable by the group.

## 5. Hardware transcoding (VAAPI)

The [transcoder](Quality-Profiles-and-Custom-Formats#transcoding-a-profiles-files) can encode on an Intel or AMD GPU. Pass the render node through, from the host:

```bash
pct set 120 -dev0 /dev/dri/renderD128,gid=$(pct exec 120 -- getent group render | cut -d: -f3)
pct reboot 120
```

(`dev0` is Proxmox VE 8.2 and later; it maps the node to the container's `render` group, which the script already added the `streamline` user to.) Then install the VA driver inside the container — `intel-media-va-driver` for Intel (Broadwell and later; if encoding fails, `intel-media-va-driver-non-free` from Debian's `non-free` component), `i965-va-driver` for older Intel chips, `mesa-va-drivers` for AMD — and restart the service. Settings → Transcoding reports whether the device answered.

Debian's `ffmpeg` has VAAPI but no `libvmaf`, so [`transcoding.verify.min_vmaf`](Configuration-Reference#transcoding) parses and is never enforced on this install.

## Updating

```bash
streamline-update
```

It fetches and verifies the latest release *before* stopping anything, so a failed download leaves the running version alone; then it stops the service, copies the database to `/var/lib/streamline/backups/` (the five newest are kept), swaps the binary, refreshes the unit and starts it again. The new version migrates the database on boot, and migrations only go forward: the backup is how you go back. `streamline-update` is the same script as the install with `update`, so `curl -fsSL …/streamline.sh | bash -s -- update` does the same thing.

Take a [Proxmox backup](https://pve.proxmox.com/wiki/Backup_and_Restore) of the container now and then too: the backup above covers a bad migration, not a lost disk.

## What the script installs

| Path | What |
| --- | --- |
| `/usr/local/bin/streamline` | The binary |
| `/etc/streamline/config.yaml` | Config, mode 0600, owned by `streamline` — the web UI writes settings back to it |
| `/var/lib/streamline` | `data_dir`: database, posters, built-in torrent state |
| `/etc/systemd/system/streamline.service` | The unit (`journalctl -u streamline -f` for logs) |
| `/usr/local/sbin/streamline-update` | The updater |

The same script works on any Debian or Ubuntu machine with systemd, a VM or bare metal included.

## Proxmox VE Helper-Scripts

[`deploy/lxc/community-scripts/`](https://github.com/DataHearth/streamline/tree/main/deploy/lxc/community-scripts) holds the same install in the [Proxmox VE Helper-Scripts](https://github.com/community-scripts/ProxmoxVE) layout (`ct/streamline.sh` creates the container, `install/streamline-install.sh` installs into it, and running `update` in the container updates it). It will be submitted there once the project meets their listing requirements; until then the container scripts can only run from their repository, so use the steps above.
