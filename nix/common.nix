# The service body both modules share. Not a module: it returns plain values
# the NixOS and home-manager modules splice into their own unit definitions,
# which is what keeps the sandboxing and the config-seeding identical between
# a system service and a user service.
{
  lib,
  pkgs,
  cfg,
}:
let
  format = pkgs.formats.yaml { };
  declaredFile = format.generate "streamline-config.yaml" cfg.settings;
in
rec {
  inherit declaredFile;

  configFile = if cfg.mutableSettings then "${cfg.stateDir}/config.yaml" else declaredFile;

  # Seeded, not synced: once the instance owns the file, rewriting it from the
  # store on every rebuild would throw away every setting made in the UI.
  preStart = ''
    mkdir -p ${lib.escapeShellArg cfg.stateDir} ${lib.escapeShellArg cfg.settings.data_dir}
    ${lib.optionalString cfg.mutableSettings ''
      if [ ! -e ${lib.escapeShellArg configFile} ]; then
        install -m 0640 ${declaredFile} ${lib.escapeShellArg configFile}
      fi
    ''}
  '';

  execStart = "${lib.getExe cfg.package} --config ${configFile}";

  settingsDefaults = {
    read_only = lib.mkIf (!cfg.mutableSettings) (lib.mkDefault true);
    ffmpeg = lib.mkIf cfg.ffmpeg.enable {
      enabled = lib.mkDefault true;
      # resolveBinary() treats this as the directory to look ffprobe up in,
      # not as the ffmpeg executable itself.
      path = lib.mkDefault "${cfg.ffmpeg.package}/bin";
    };
  };

  serviceConfig = {
    ExecStartPre = "${pkgs.writeShellScript "streamline-pre-start" preStart}";
    ExecStart = execStart;
    WorkingDirectory = cfg.stateDir;
    Restart = "on-failure";
    RestartSec = 5;

    EnvironmentFile = lib.mkIf (cfg.environmentFile != null) [ cfg.environmentFile ];
    LoadCredential = lib.mapAttrsToList (name: path: "${name}:${path}") cfg.credentials;

    LockPersonality = true;
    MemoryDenyWriteExecute = true;
    NoNewPrivileges = true;
    PrivateTmp = true;
    ProtectClock = true;
    ProtectControlGroups = true;
    ProtectHostname = true;
    ProtectKernelLogs = true;
    ProtectKernelModules = true;
    ProtectKernelTunables = true;
    ProtectProc = "invisible";
    RemoveIPC = true;
    RestrictAddressFamilies = [
      "AF_INET"
      "AF_INET6"
      "AF_UNIX"
    ];
    RestrictNamespaces = true;
    RestrictRealtime = true;
    RestrictSUIDSGID = true;
    SystemCallArchitectures = "native";
    SystemCallFilter = [
      "@system-service"
      "~@privileged"
      "~@resources"
    ];
    UMask = "0027";
  };
}
