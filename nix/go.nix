{
  lib,
  buildPackages,
}:
# flake.lock's nixpkgs decides the Go toolchain; go.mod only records which
# version that is. `.github/workflows/renovate-pins.yaml` rewrites the directive
# from the locked package whenever renovate moves the lock, and CI installs from
# go.mod through `go-version-file` — so the devshell, `nix build`, CI and the
# container all land on one version.
#
# Exact, not a floor. A devshell a patch away from what CI builds with is not a
# reproduction of it, so a mismatch throws here rather than surfacing later as a
# toolchain error with nothing in it naming the two versions that disagree. Only
# the patch is ever synced: the minor is a deliberate choice and stays wherever
# the directive puts it.
let
  declared = lib.removePrefix "go " (
    lib.findFirst (lib.hasPrefix "go ") (throw "no `go` directive in go.mod") (
      lib.splitString "\n" (builtins.readFile ../go.mod)
    )
  );
  attr = "go_${lib.replaceStrings [ "." ] [ "_" ] (lib.versions.majorMinor declared)}";

  pkg = lib.throwIf (!buildPackages ? ${attr}) ''
    go.mod declares Go ${declared}, but nixpkgs has no ${attr}.
    Wait for nixpkgs, or hold the directive on a minor it packages.
  '' buildPackages.${attr};
in
lib.throwIf (pkg.version != declared) ''
  go.mod declares Go ${declared}, but ${attr} in the locked nixpkgs is ${pkg.version}.
  The devshell has to be the version the software is built with, not near it.
  Move the lock (`nix flake update nixpkgs`) and let the declarations be
  rewritten from it, or put the directive back on ${pkg.version}.
'' pkg
