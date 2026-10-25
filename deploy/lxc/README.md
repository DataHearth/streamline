# LXC install

Install and update scripts for running Streamline in a Proxmox VE LXC container (or on any Debian or Ubuntu host with systemd). The user-facing walkthrough — media mounts, unprivileged ID mapping, VAAPI passthrough — is the [Proxmox wiki page](https://github.com/DataHearth/streamline/wiki/Proxmox).

| File | What it is |
| --- | --- |
| `streamline.sh` | Standalone installer and updater, run as root inside the container: `curl -fsSL …/streamline.sh \| bash`, then `streamline-update` (the same script with `update`). Works today. |
| `community-scripts/ct/streamline.sh` | Container script in the [Proxmox VE Helper-Scripts](https://github.com/community-scripts/ProxmoxVE) layout: creates the LXC and carries `update_script`. |
| `community-scripts/install/streamline-install.sh` | Its install script, run inside the new container by their framework. |

## Why two installs

The Helper-Scripts framework fetches `install/<app>-install.sh` from its own repository, so the `community-scripts/` pair cannot run from here. It is kept submission-ready: their request form needs 1,000 stars on the source repository and six months of age, and new scripts go through their DevScripts repository first. Until then `streamline.sh` is the install users run.

Both do the same thing, and a change to one belongs in the other:

- download the release archive for the architecture and check it against `checksums.txt` — the Helper-Scripts' `fetch_and_deploy_gh_release` does not, which is why neither uses it; `streamline.sh` also checks the cosign signature on `checksums.txt` when `cosign` is installed (the identity is case-sensitive: `https://github.com/DataHearth/streamline/.github/workflows/release.yaml@refs/tags/v…`);
- install the binary to `/usr/local/bin`, `ffmpeg` from the distribution, a `streamline` system user (in `render`/`video`), `/etc/streamline/config.yaml` from `streamline config init` with `data_dir: /var/lib/streamline`, mode 0600;
- install the systemd unit: `ProtectSystem=full` with `/etc/streamline` and `/var/lib/streamline` writable, rather than `strict`, because the media mounts are the user's choice and a mount missing from `ReadWritePaths` fails imports far from the cause; no `MemoryDenyWriteExecute` or syscall filter, since the TagLib engine runs as WebAssembly under wazero's compiler; not `PrivateDevices`, which would hide `/dev/dri`;
- on update, fetch and verify before stopping the service, then copy the database aside (`<data_dir>/backups/`) before the new binary migrates it.

## Submitting to Helper-Scripts

Their framework moves (it is mid-way from `misc/*.func` in ProxmoxVE to the `community-scripts/core` repository): before submitting, compare `community-scripts/` with a current `ct/<app>.sh` and `install/<app>-install.sh` of a similar prebuilt-binary app (autobrr is close) and with their contribution guide, and add the JSON metadata file their website reads.
