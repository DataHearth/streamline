# NixOS and Nix

Streamline ships a flake with a package, a NixOS module, a home-manager module and a VM test that boots the service for real. Nothing here wraps the Docker image — the package builds the binary from source, frontend and all, and the module runs it as a systemd unit.

- [Try it without installing](#try-it-without-installing)
- [Adding the flake](#adding-the-flake)
- [NixOS module](#nixos-module)
- [Mutable vs declarative settings](#mutable-vs-declarative-settings)
- [Secrets](#secrets)
- [ffmpeg and hardware transcoding](#ffmpeg-and-hardware-transcoding)
- [Sandboxing and media paths](#sandboxing-and-media-paths)
- [home-manager](#home-manager)
- [Option reference](#option-reference)

---

## Try it without installing

```bash
nix run github:DataHearth/streamline -- config init --output ./config.yaml
nix run github:DataHearth/streamline -- --config ./config.yaml
```

The binary reports its version as the release the flake was tagged at; `nix build` has no git tag to read, so it is stamped from `nix/package.nix` rather than from the commit.

## Adding the flake

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    streamline = {
      url = "github:DataHearth/streamline";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, streamline, ... }: {
    nixosConfigurations.media = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        streamline.nixosModules.default
        { nixpkgs.overlays = [ streamline.overlays.default ]; }
        ./configuration.nix
      ];
    };
  };
}
```

The overlay is what puts `pkgs.streamline` in scope, which is where the module's `package` option defaults. Outputs available: `packages.<system>.streamline`, `overlays.default`, `nixosModules.streamline`, `homeModules.streamline`, and `checks.<system>.{build,nixos,home-manager,lint}`.

Supported systems are `x86_64-linux` and `aarch64-linux`.

## NixOS module

```nix
services.streamline = {
  enable = true;
  openFirewall = true;

  settings = {
    server.host = "0.0.0.0";
    library = {
      movie_path = "/srv/media/movies";
      series_path = "/srv/media/series";
      download_path = "/srv/downloads";
    };
    auth.seed_admin.email = "admin@example.com";
    auth.seed_admin.password_file = "/run/credentials/streamline.service/seed-password";
  };

  credentials.seed-password = "/run/secrets/streamline-admin";
};
```

`settings` is a freeform attribute set rendered straight to YAML, so every key in the [Configuration Reference](Configuration-Reference) works here under its own name — `library.import_mode`, `schedules.movie_rss_sync`, `quality_profiles`, all of it. Only `data_dir`, `server.host` and `server.port` are declared with defaults; everything else falls through to Streamline's own.

The service runs as the system user `streamline` with state under `/var/lib/streamline`. **That user must be able to read and write your media and download directories** — see [the folder rule](Installation#before-you-start-the-folder-rule), which applies here exactly as it does under Docker.

## Mutable vs declarative settings

Streamline's settings pages write back into the config file: adding an indexer, editing a quality profile, or letting it mint a Plex client id all rewrite the YAML. A file in the Nix store cannot be rewritten, so the module offers both models through `mutableSettings`.

**`mutableSettings = true` (the default).** Your `settings` are copied to `/var/lib/streamline/config.yaml` the first time the service starts, and the instance owns the file from then on. The web UI works in full.

> A later `nixos-rebuild` does **not** overwrite that file. Once seeded, Nix is no longer the source of truth for settings — delete `/var/lib/streamline/config.yaml` and restart to re-seed from your declaration.

**`mutableSettings = false`.** The generated file is passed straight out of the store and `read_only` is forced on. Every write-back is refused cleanly with a read-only error rather than failing on a read-only filesystem, and every settings page in the UI locks. This is the same posture the Helm chart takes — see [GitOps and Kubernetes](GitOps-and-Kubernetes) for what a fully declarative deploy has to supply by hand, notably the session secret and the Plex client id the app would otherwise generate for itself.

## Secrets

Never put a secret in `settings` — it would land world-readable in the Nix store. Every secret in Streamline has a `*_file` twin, and the module passes files to the unit through systemd's `LoadCredential`:

```nix
services.streamline = {
  credentials = {
    seed-password = config.age.secrets.streamline-admin.path;
    tmdb-key = config.age.secrets.tmdb.path;
    session-secret = config.age.secrets.streamline-session.path;
  };

  settings = {
    auth.seed_admin.password_file = "/run/credentials/streamline.service/seed-password";
    auth.session_secret_file = "/run/credentials/streamline.service/session-secret";
    metadata.tmdb_api_key_file = "/run/credentials/streamline.service/tmdb-key";
  };
};
```

The credential is readable by the service alone, and the path is always `/run/credentials/streamline.service/<name>`.

For anything without a `*_file` twin, `environmentFile` takes a file of `STREAMLINE_*` assignments read by systemd, which never enters the store:

```
STREAMLINE_AUTH__SESSION_SECRET=…
```

A double underscore is the section separator: `STREAMLINE_AUTH__SESSION_SECRET` sets `auth.session_secret`.

## ffmpeg and hardware transcoding

`ffmpeg.enable` is on by default and puts `pkgs.ffmpeg-headless` on the unit's `PATH`, with `ffmpeg.path` pointed at its `bin` directory. That covers media probing and software transcoding. See [Optional: ffmpeg](Installation#optional-ffmpeg) for what the three gated features actually are.

For VAAPI:

```nix
services.streamline = {
  hardwareAcceleration.enable = true;
  ffmpeg.package = pkgs.ffmpeg-full;
  settings.transcoding = {
    enabled = true;
    hw_accel = "vaapi";
  };
};
```

Two separate things, and both are needed:

- `hardwareAcceleration.enable` opens `/dev/dri` to the unit and adds the service user to the `render` and `video` groups. Without it the sandbox blocks the device outright.
- `ffmpeg-headless` **cannot use the device even when it is open** — it is built without libva. `ffmpeg-full` is the build that has VAAPI and libvmaf compiled in, and without libvmaf `transcoding.verify.min_vmaf` parses, stores, and silently never runs.

`hardwareAcceleration.device` (default `/dev/dri/renderD128`) also fills in `transcoding.hw_device`.

## Sandboxing and media paths

The unit runs with `ProtectSystem = "strict"`, so the only writable paths are the ones the module grants. It always grants the state dir plus the absolute `data_dir`, `library.movie_path`, `library.series_path` and `library.download_path` from your `settings` — the four Streamline actually writes through.

Add anything else the service must write to:

```nix
services.streamline.extraReadWritePaths = [ "/mnt/media2" ];
```

This **adds** to the derived set rather than replacing it, so you never have to restate the paths already in `settings`.

Two consequences worth knowing before you debug them:

- A path listed here that does not exist when the unit starts makes it fail with a mount-namespace error, not a Streamline error. A late network mount needs an ordering dependency on its own unit.
- If you configure a second library root only through the web UI (with `mutableSettings = true`), the module never sees it and never grants it. Add it to `extraReadWritePaths` yourself.

## home-manager

For running Streamline under your own account — the usual reason being a library that lives in `$HOME`:

```nix
{
  imports = [ streamline.homeModules.default ];

  services.streamline = {
    enable = true;
    settings.library.movie_path = "/home/me/media/movies";
  };
}
```

It is the same option set minus the system-only ones (`user`, `group`, `openFirewall`, `hardwareAcceleration`, `extraReadWritePaths`), and it produces a systemd **user** unit with state under `$XDG_DATA_HOME/streamline`. `ProtectHome` is deliberately not set — hiding `$HOME` would hide the library the whole arrangement exists to reach.

A user unit stops when you log out unless you enable lingering: `loginctl enable-linger $USER`.

## Option reference

| Option | Default | What it does |
| --- | --- | --- |
| `enable` | `false` | Turn the service on |
| `package` | `pkgs.streamline` | Which build to run |
| `settings` | `{ }` | Freeform config, rendered to YAML |
| `mutableSettings` | `true` | Seed the config into the state dir instead of serving it from the store |
| `stateDir` | `/var/lib/streamline` | Database, and the seeded config file |
| `credentials` | `{ }` | `name = path` pairs passed via `LoadCredential` |
| `environmentFile` | `null` | File of `STREAMLINE_*` assignments |
| `user` / `group` | `streamline` | Identity the unit runs as (NixOS only) |
| `openFirewall` | `false` | Open `settings.server.port` (NixOS only) |
| `extraReadWritePaths` | `[ ]` | Writable paths on top of the derived set (NixOS only) |
| `ffmpeg.enable` | `true` | Put ffmpeg/ffprobe on the unit's `PATH` |
| `ffmpeg.package` | `pkgs.ffmpeg-headless` | Which ffmpeg build |
| `hardwareAcceleration.enable` | `false` | Open `/dev/dri` and join `render`/`video` (NixOS only) |
| `hardwareAcceleration.device` | `/dev/dri/renderD128` | Render node, also used for `transcoding.hw_device` |
