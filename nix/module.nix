{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.streamline;
  common = import ./common.nix { inherit lib pkgs cfg; };

  # library.download_path is also where torrents land, so it needs write access
  # alongside the two media roots. A relative path (the `./data` style default)
  # is not something the sandbox can grant.
  derivedPaths = lib.filter (p: p != null && lib.hasPrefix "/" p) [
    cfg.settings.data_dir or null
    cfg.settings.library.movie_path or null
    cfg.settings.library.series_path or null
    cfg.settings.library.download_path or null
  ];
in
{
  imports = [ ./options.nix ];

  options.services.streamline = {
    user = lib.mkOption {
      type = lib.types.str;
      default = "streamline";
      description = "User account the service runs as. Must be able to read and write the media library.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "streamline";
      description = "Group the service runs as.";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open {option}`services.streamline.settings.server.port` in the firewall.";
    };

    hardwareAcceleration = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = ''
          Grant the service access to the DRM render node for VAAPI transcoding.
          Adds the user to the `render` and `video` groups and opens `/dev/dri`
          to the unit; the sandbox blocks the device otherwise.

          This only opens the device. `ffmpeg-headless` cannot use it — set
          {option}`services.streamline.ffmpeg.package` to a build with VAAPI
          compiled in, such as `pkgs.ffmpeg-full`.
        '';
      };

      device = lib.mkOption {
        type = lib.types.str;
        default = "/dev/dri/renderD128";
        description = "Render node handed to ffmpeg, also used as {option}`services.streamline.settings.transcoding.hw_device`.";
      };
    };

    extraReadWritePaths = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "/mnt/media2" ];
      description = ''
        Extra paths the sandboxed unit may write to. The state dir and the
        absolute `data_dir`, `library.movie_path`, `library.series_path` and
        `library.download_path` from
        {option}`services.streamline.settings` are always granted; this adds to
        them rather than replacing them.

        A second library root, a bind mount, or any path configured through the
        web UI rather than through `settings` belongs here — the sandbox is
        `ProtectSystem = "strict"`, so anything ungranted is read-only.

        ::: {.warning}
        A path listed here that does not exist when the unit starts fails it
        with a mount-namespace error, not a streamline error. A late network
        mount needs an ordering dependency on its own unit.
        :::
      '';
    };
  };

  config = lib.mkMerge [
    { services.streamline.stateDir = lib.mkDefault "/var/lib/streamline"; }

    (lib.mkIf cfg.enable {
      services.streamline.settings = common.settingsDefaults // {
        transcoding = lib.mkIf cfg.hardwareAcceleration.enable {
          hw_device = lib.mkDefault cfg.hardwareAcceleration.device;
        };
      };

      users.users = lib.mkIf (cfg.user == "streamline") {
        streamline = {
          isSystemUser = true;
          inherit (cfg) group;
          home = cfg.stateDir;
          extraGroups = lib.optionals cfg.hardwareAcceleration.enable [
            "render"
            "video"
          ];
        };
      };

      users.groups = lib.mkIf (cfg.group == "streamline") { streamline = { }; };

      networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.settings.server.port ];

      systemd.tmpfiles.settings."10-streamline" =
        lib.genAttrs
          (lib.unique [
            cfg.stateDir
            cfg.settings.data_dir
          ])
          (_: {
            d = {
              inherit (cfg) user group;
              mode = "0750";
            };
          });

      systemd.services.streamline = {
        description = "Streamline media management platform";
        wantedBy = [ "multi-user.target" ];
        after = [ "network-online.target" ];
        wants = [ "network-online.target" ];

        path = lib.optional cfg.ffmpeg.enable cfg.ffmpeg.package;

        serviceConfig = common.serviceConfig // {
          User = cfg.user;
          Group = cfg.group;

          ReadWritePaths = lib.unique (derivedPaths ++ cfg.extraReadWritePaths ++ [ cfg.stateDir ]);
          ProtectSystem = "strict";
          ProtectHome = true;

          SupplementaryGroups = lib.optionals cfg.hardwareAcceleration.enable [
            "render"
            "video"
          ];
          DeviceAllow = lib.optionals cfg.hardwareAcceleration.enable [ "/dev/dri rw" ];
          PrivateDevices = !cfg.hardwareAcceleration.enable;

          AmbientCapabilities = lib.optional (cfg.settings.server.port < 1024) "CAP_NET_BIND_SERVICE";
          CapabilityBoundingSet = lib.optional (cfg.settings.server.port < 1024) "CAP_NET_BIND_SERVICE";
        };
      };
    })
  ];
}
