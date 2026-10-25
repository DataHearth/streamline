# Unraid template

`streamline.xml` is an Unraid [Community Applications](https://forums.unraid.net/topic/38582-plug-in-community-applications/) template (Container version 2) for the official image. The user-facing walkthrough is the [Installation wiki](https://github.com/DataHearth/streamline/wiki/Installation#unraid); this file is for whoever maintains the template.

## What it sets up

| Field | Value | Why |
| --- | --- | --- |
| Repository | `ghcr.io/datahearth/streamline:latest` | |
| Extra parameters | `--user 99:100` | Unraid shares and appdata belong to `nobody:users`; the image's own `1000:1000` cannot write them |
| `/mnt/user/data` → `/mnt/user/data` | the data share | downloads and media on one share, same path inside and out, so imports hardlink and the torrent client's paths resolve unchanged |
| `/etc/streamline` → `appdata/streamline/config` | config | `config.yaml` is written there on first start |
| `/data` → `appdata/streamline/data` | app data | database, posters, built-in torrent state |
| `STREAMLINE_LIBRARY__*_PATH` | library folders | the six library paths are config-file only; set by environment, they are fixed by the template |

First start needs nothing else: when `--config` names a missing file, Streamline writes the `config init` defaults there, with the library folders the environment sets (`config.Seed`), then generates the session secret into it. The seed admin's generated password is printed once to the container log, never to the file.

## Installing it before it is listed

```bash
wget -O /boot/config/plugins/dockerMan/templates-user/my-streamline.xml \
  https://raw.githubusercontent.com/DataHearth/streamline/main/deploy/unraid/streamline.xml
```

Then **Docker → Add Container → Template → streamline**.

## Listing it in Community Applications

CA indexes template repositories that are registered with it, not single files. To list this one:

1. Submit the repository through the template submission process in the Community Applications policies on the Unraid forums, pointing at this repository (CA picks up `deploy/unraid/streamline.xml`).
2. CA asks for a support thread: the template's `<Support>` points at [GitHub Discussions](https://github.com/DataHearth/streamline/discussions). Open an *Unraid* discussion and point `<Support>` at it once CA accepts the template, or open a thread in the Unraid forum's Docker Containers board.
3. Keep `<TemplateURL>` on `main`: Unraid re-reads the template from it on every update check, so a field added here reaches installed containers.

## Changing it

- Every `<Config>` needs `Default` and a body value; Unraid shows the body and resets to `Default`.
- A path the app needs to write must stay writable by `99:100`. Docker creates a missing bind-mount source as `root`, so a folder that did not exist before the first start may need `chown -R 99:100 /mnt/user/appdata/streamline`.
- Don't add a `<Shell>`: the image is distroless and has none, so Unraid's console button could only fail.
