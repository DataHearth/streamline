{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.streamline;
  common = import ./common.nix { inherit lib pkgs cfg; };
in
{
  imports = [ ./options.nix ];

  config = lib.mkMerge [
    { services.streamline.stateDir = lib.mkDefault "${config.xdg.dataHome}/streamline"; }

    (lib.mkIf cfg.enable {
      assertions = [
        {
          assertion = pkgs.stdenv.hostPlatform.isLinux;
          message = "services.streamline uses a systemd user unit and is Linux-only.";
        }
      ];

      services.streamline.settings = common.settingsDefaults;

      home.packages = [ cfg.package ];

      systemd.user.services.streamline = {
        Unit = {
          Description = "Streamline media management platform";
          After = [ "network-online.target" ];
          Wants = [ "network-online.target" ];
        };

        Install.WantedBy = [ "default.target" ];

        # No ProtectSystem/ProtectHome here, unlike the NixOS module: the whole
        # point of running it under your own account is reaching a library that
        # lives in $HOME, and ProtectHome=true would hide exactly that.
        Service = common.serviceConfig // {
          Environment = lib.optional cfg.ffmpeg.enable "PATH=${cfg.ffmpeg.package}/bin";
        };
      };
    })
  ];
}
