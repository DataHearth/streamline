# Option declarations shared by the NixOS and home-manager modules. Both put
# streamline under `services.streamline`, so the tree is identical; only the
# config half (system unit vs user unit, user/group, firewall) differs.
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.streamline;
  format = pkgs.formats.yaml { };
in
{
  options.services.streamline = {
    enable = lib.mkEnableOption "streamline, a unified media management platform";

    package = lib.mkPackageOption pkgs "streamline" { };

    stateDir = lib.mkOption {
      type = lib.types.str;
      description = "Directory holding the SQLite database, and the config file when {option}`services.streamline.mutableSettings` is enabled.";
    };

    mutableSettings = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Whether the running instance may rewrite its own config file.

        Streamline's settings pages call back into the config file: changing a
        quality profile, adding an indexer or letting it mint a Plex client id
        all rewrite the YAML. A file in the Nix store cannot be rewritten, so
        the two modes differ in what the web UI can do:

        - `true` (default): {option}`services.streamline.settings` is copied to
          `''${stateDir}/config.yaml` the first time the service starts, and the
          instance owns it from then on. The UI works in full. A later rebuild
          does **not** overwrite the file — delete it to re-seed from the Nix
          declaration.
        - `false`: the generated file is passed straight from the store and
          `read_only` is forced on, so every write-back is refused cleanly
          instead of failing on a read-only filesystem. Every settings page in
          the UI locks, and nothing drifts from the declaration.
      '';
    };

    settings = lib.mkOption {
      type = lib.types.submodule {
        freeformType = format.type;

        options = {
          data_dir = lib.mkOption {
            type = lib.types.str;
            default = "${cfg.stateDir}/data";
            defaultText = lib.literalExpression ''"''${config.services.streamline.stateDir}/data"'';
            description = "Directory for the SQLite database and other persistent state.";
          };

          server.host = lib.mkOption {
            type = lib.types.str;
            default = "127.0.0.1";
            description = "Address to bind the HTTP server to.";
          };

          server.port = lib.mkOption {
            type = lib.types.port;
            default = 8080;
            description = "Port to serve the web UI and REST API on.";
          };
        };
      };
      default = { };
      example = lib.literalExpression ''
        {
          server.host = "0.0.0.0";
          library = {
            movie_path = "/srv/media/movies";
            series_path = "/srv/media/series";
            download_path = "/srv/downloads";
          };
          auth.seed_admin = {
            email = "admin@example.com";
            password_file = "/run/credentials/streamline.service/seed-password";
          };
        }
      '';
      description = ''
        Streamline configuration, rendered to YAML. See
        <https://github.com/DataHearth/streamline/blob/main/api/config.schema.json>
        for the full surface.

        Every secret has a `*_file` twin (`auth.session_secret_file`,
        `metadata.tmdb_api_key_file`, `auth.oidc.*.client_secret_file`, …) —
        use those with {option}`services.streamline.credentials` rather than
        putting the value here, which would land it in the world-readable Nix
        store.
      '';
    };

    credentials = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      example = lib.literalExpression ''
        {
          seed-password = "/run/secrets/streamline-admin-password";
          tmdb-key = "/run/secrets/tmdb-api-key";
        }
      '';
      description = ''
        Files passed to the unit through systemd's `LoadCredential`, readable
        by the service alone at
        `/run/credentials/streamline.service/''${name}`. Point the config's
        `*_file` keys at those paths.
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = ''
        Path to a file of `STREAMLINE_*` environment assignments, merged over
        the config file at load time (`STREAMLINE_AUTH__SESSION_SECRET` sets
        `auth.session_secret`). Read by systemd, so it never enters the store.
      '';
    };

    ffmpeg = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Provide ffmpeg/ffprobe to the service. Media probing and transcoding are inert without them.";
      };

      package = lib.mkOption {
        type = lib.types.package;
        default = pkgs.ffmpeg-headless;
        defaultText = lib.literalExpression "pkgs.ffmpeg-headless";
        description = ''
          ffmpeg build to use. The headless default carries no X11/SDL outputs
          and is enough for probing and software transcoding; `ffmpeg-full` is
          what has VAAPI and libvmaf compiled in, so
          {option}`services.streamline.settings.transcoding.verify.min_vmaf`
          silently never runs without it.
        '';
      };
    };
  };
}
