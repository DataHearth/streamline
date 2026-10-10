{
  lib,
  buildPackages,
  fetchurl,
}:
# go.mod's `go` directive decides the Go version — renovate bumps it from
# go.dev's own release list the day a patch ships. The locked nixpkgs only
# decides which lines exist: the minor has to be packaged as `go_1_NN`, or this
# throws. Within the line, nixpkgs' package is used as-is when it is already on
# the declared patch and overridden onto it otherwise (either direction), so
# the devshell, `nix build`, CI and the container still run one version — the
# declared one — without a security release waiting on a nixpkgs channel.
#
# The override reuses nixpkgs' derivation and patches with upstream's source
# tarball, whose hash lives in toolchains.json keyed by version.
# `.github/workflows/renovate-pins.yaml` writes it on renovate's branch, and an
# entry is only read while the override is in force, so one nixpkgs has caught
# up with is dead weight the same workflow prunes.
let
  declared = lib.removePrefix "go " (
    lib.findFirst (lib.hasPrefix "go ") (throw "no `go` directive in go.mod") (
      lib.splitString "\n" (builtins.readFile ../go.mod)
    )
  );
  attr = "go_${lib.replaceStrings [ "." ] [ "_" ] (lib.versions.majorMinor declared)}";

  pkg = lib.throwIf (!buildPackages ? ${attr}) ''
    go.mod declares Go ${declared}, but nixpkgs has no ${attr}.
    A new Go minor waits for nixpkgs to package it: hold the directive on a
    minor the locked nixpkgs has, or move the lock once it ships ${attr}.
  '' buildPackages.${attr};

  hash =
    (lib.importJSON ./toolchains.json).go.${declared} or (throw ''
      go.mod declares Go ${declared}, but the locked nixpkgs has ${pkg.version},
      and nix/toolchains.json has no source hash for ${declared}.
      Add it under "go": `nix store prefetch-file https://go.dev/dl/go${declared}.src.tar.gz`.
    '');
in
if pkg.version == declared then
  pkg
else
  pkg.overrideAttrs {
    version = declared;
    src = fetchurl {
      url = "https://go.dev/dl/go${declared}.src.tar.gz";
      inherit hash;
    };
  }
