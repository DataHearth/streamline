{
  lib,
  buildPackages,
}:
# Same contract as nix/go.nix: flake.lock's nixpkgs decides, engines.node
# records. `.github/workflows/renovate-pins.yaml` rewrites the floor from the
# locked package, and setup-node and the container base image are written from
# it in the same step.
#
# `>=` is for anyone building without the devshell — they owe the project at
# least this version and may be ahead of it. The devshell owes it exactly, so
# the floor is what this compares against. The operator has to come off before
# the string is parsed: `lib.versions.major ">=24.20.0"` is `">=24"`, which
# would look up an attribute that does not exist.
let
  declared = lib.removePrefix ">=" (lib.importJSON ../package.json).engines.node;
  attr = "nodejs_${lib.versions.major declared}";

  pkg = lib.throwIf (!buildPackages ? ${attr}) ''
    package.json declares Node ${declared}, but nixpkgs has no ${attr}.
    Wait for nixpkgs, or hold engines.node on a major it packages.
  '' buildPackages.${attr};
in
lib.throwIf (pkg.version != declared) ''
  package.json declares Node ${declared}, but ${attr} in the locked nixpkgs is ${pkg.version}.
  The devshell has to be the version the software is built with, not near it —
  pnpm enforces a project's own engines field unconditionally, so a drift here
  is an ERR_PNPM_UNSUPPORTED_ENGINE inside the devshell itself.
  Move the lock (`nix flake update nixpkgs`) and let the declarations be
  rewritten from it, or put the floor back on ${pkg.version}.
'' pkg
