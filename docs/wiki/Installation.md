# Installation

Streamline is a single binary with no external dependencies — no database server, no runtime, no CGO. Pick whichever install method matches how you already run things.

- [Before you start: the folder rule](#before-you-start-the-folder-rule)
- [Docker Compose](#docker-compose) — recommended for most people
- [Plain binary](#plain-binary)
- [Unraid, Synology, TrueNAS](#unraid-synology-truenas)
- [Kubernetes / Helm](#kubernetes--helm)
- [Verifying what you downloaded](#verifying-what-you-downloaded)

---

## Before you start: the folder rule

This is the single most common setup mistake, so it goes first.

Streamline needs two directories:

- **Downloads** — where your torrent client puts finished files
- **Media** — where your organised library lives, the folder Plex/Jellyfin/Emby reads

By default Streamline **hardlinks** files from downloads into media. A hardlink is a second name for the same data on disk: the file appears in both places but occupies space once, and your torrent client can keep seeding the original untouched.

Hardlinks only work **within one filesystem**. If downloads and media are on different disks, different volumes, or mounted into the container as two unrelated paths, hardlinking fails and Streamline falls back to erroring out rather than silently doubling your disk usage.

**The fix is to mount one parent directory, not two children.** Do this:

```yaml
volumes:
  - /srv/data:/data-root          # contains both media/ and downloads/
```

Not this:

```yaml
volumes:
  - /srv/data/media:/media        # ✗ two separate mounts — different
  - /srv/data/downloads:/downloads # ✗ filesystems as far as the container knows
```

Your torrent client needs the *same* layout mounted at the *same* paths, or the paths it reports won't resolve inside Streamline's container.

If you genuinely can't put them on one filesystem, set `library.import_mode` to `copy` (keeps the torrent seeding, uses double the space) or `move` (saves space, kills seeding). See the [Configuration Reference](Configuration-Reference#library).

---

## Docker Compose

### 1. Create the config

```bash
mkdir -p config data
docker run --rm -v "$PWD/config:/etc/streamline" \
  ghcr.io/datahearth/streamline:latest \
  config init --output /etc/streamline/config.yaml
```

This writes a fully-commented config file with every key at its default. You'll edit it in [First-Run Setup](First-Run-Setup).

### 2. Write `compose.yaml`

```yaml
services:
  streamline:
    image: ghcr.io/datahearth/streamline:latest
    container_name: streamline
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
      - ./config:/etc/streamline
      # One mount covering both media and downloads — see the folder rule above.
      - /srv/data:/srv/data
```

Then tell Streamline where things live, in `config/config.yaml`:

```yaml
library:
  movie_path: /srv/data/media/movies
  series_path: /srv/data/media/series
  download_path: /srv/data/downloads
```

### 3. Start it

```bash
docker compose up -d
docker compose logs -f streamline
```

Open <http://localhost:8080>.

> **Mounting the config read-only?** `:ro` is safe for a GitOps-style deploy, but Streamline writes a few generated values back on first boot (session signing secret, Plex client ID, and a generated admin password if you didn't set one). With a read-only mount you must supply those yourself — see [GitOps and Kubernetes](GitOps-and-Kubernetes).

### Image tags

| Tag | What it tracks |
| --- | --- |
| `latest` | Most recent stable release |
| `vX.Y.Z`, `X.Y`, `X` | Pinned release / minor line / major line |
| `edge` | Every push to `main` — expect breakage |
| `sha-<short>` | An exact commit |

Pin to `vX.Y.Z` or at least `X.Y` for anything you care about.

---

## Plain binary

Requires nothing but the binary itself. Grab it from the [Releases page](https://github.com/datahearth/streamline/releases/latest) — Linux, macOS and Windows, amd64 and arm64.

```bash
# Linux amd64 — substitute the real version number
curl -fsSL -o streamline.tar.gz \
  https://github.com/datahearth/streamline/releases/latest/download/streamline_<version>_linux_amd64.tar.gz
tar xzf streamline.tar.gz

mkdir -p ~/.config/streamline
cp config.example.yaml ~/.config/streamline/config.yaml

./streamline --config ~/.config/streamline/config.yaml
```

Each archive ships a `config.example.yaml` with every key at its default value.

### Running it as a service (systemd)

```ini
# /etc/systemd/system/streamline.service
[Unit]
Description=Streamline
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=streamline
Group=streamline
ExecStart=/opt/streamline/streamline --config /etc/streamline/config.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd --system --home /var/lib/streamline --shell /usr/sbin/nologin streamline
sudo mkdir -p /var/lib/streamline /etc/streamline
sudo chown -R streamline:streamline /var/lib/streamline
sudo systemctl enable --now streamline
sudo journalctl -u streamline -f
```

Set `data_dir: /var/lib/streamline` in your config so the database lands somewhere the service user can write.

---

## Unraid, Synology, TrueNAS

There's no first-party app-store package for these yet — you install the Docker image through whatever container UI your NAS provides. The settings translate like this:

| Field in your NAS's Docker UI | Value |
| --- | --- |
| Repository / Image | `ghcr.io/datahearth/streamline:latest` |
| Port | `8080` → `8080` |
| Volume: config | host `…/streamline/config` → container `/etc/streamline` |
| Volume: data | host `…/streamline/data` → container `/data` |
| Volume: media + downloads | **one** host share (e.g. `/mnt/user/data`) → same path in container |
| Restart policy | Unless stopped |

**Unraid specifics.** Use `/mnt/user/data` as the single mount rather than separate `/mnt/user/media` and `/mnt/user/downloads` shares — the folder rule above applies with full force, and Unraid users hit it constantly. Set the container path identical to the host path so paths reported by your torrent client resolve unchanged.

**Synology specifics.** Container Manager (or Docker on older DSM) works fine. Create one shared folder holding both `media` and `downloads` subfolders. Note that Synology's Btrfs shares do support hardlinks, but only within a single shared folder — across two shared folders they'll fail.

**TrueNAS SCALE specifics.** Either use the Docker/Apps custom-app flow with the same mounts, or install the [Helm chart](#kubernetes--helm) directly since SCALE runs Kubernetes underneath.

You'll then need to generate a config file. The simplest route is to start the container once, let it fail or come up on defaults, then edit `config/config.yaml` on the NAS filesystem directly and restart.

---

## Kubernetes / Helm

```bash
helm install streamline oci://ghcr.io/datahearth/charts/streamline \
  --namespace streamline --create-namespace \
  --set image.tag=X.Y.Z
```

Pin a chart version with `--version X.Y.Z`.

**`image.tag` is required** (chart 2.2.0+): the chart never picks a Streamline version for you — you choose the app release to deploy and bump it yourself to upgrade. Installing without it fails with `image.tag is required`. App releases are tagged `vX.Y.Z` (the image tag is `X.Y.Z`), chart releases `chart-vX.Y.Z`.

The chart is versioned **independently** of Streamline itself: a chart fix ships without an app release and vice versa. `--version` selects the chart; `image.tag` selects the app.

Two things about the chart that surprise people:

- **`replicaCount` is 1 and must stay 1.** Streamline stores state in SQLite, which is a single-writer database. A second replica will corrupt it.
- **The chart defaults to `read_only: true`.** Config changes flow through git, not the web UI; the settings pages will reject writes. This is deliberate for declarative deploys — see [GitOps and Kubernetes](GitOps-and-Kubernetes) for the full treatment, including how to supply the secrets the app would normally generate for itself.

---

## Verifying what you downloaded

Every release artefact is signed. If you care about supply-chain integrity, verify before running.

**Binaries.** `checksums.txt` is signed with [cosign](https://github.com/sigstore/cosign) (keyless, via GitHub OIDC). Verify the signature first, then the hashes:

```bash
curl -fsSL -O https://github.com/datahearth/streamline/releases/latest/download/checksums.txt
curl -fsSL -O https://github.com/datahearth/streamline/releases/latest/download/checksums.txt.bundle

cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --certificate-identity-regexp="https://github.com/datahearth/streamline/.github/workflows/release.yaml@.*" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com

sha256sum -c checksums.txt --ignore-missing
```

**Images.**

```bash
cosign verify ghcr.io/datahearth/streamline:latest \
  --certificate-identity-regexp="https://github.com/datahearth/streamline/.github/workflows/image.yaml@.*" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

**SBOMs.** Each archive ships an SPDX SBOM alongside it (`<archive>.sbom.spdx.json`); images carry theirs as a cosign attestation:

```bash
cosign download attestation ghcr.io/datahearth/streamline:latest \
  --predicate-type=https://spdx.dev/Document
```

Every image push is also scanned by [grype](https://github.com/anchore/grype) at severity ≥ high, with results published to the repository's [Security tab](https://github.com/datahearth/streamline/security/code-scanning).

---

**Next:** [First-Run Setup](First-Run-Setup) — logging in and connecting your indexers and download client.
