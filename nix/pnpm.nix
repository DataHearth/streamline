{
  lib,
  buildPackages,
}:
# Same contract as nix/go.nix: package.json's packageManager decides the
# version (renovate bumps it from the npm registry, together with CI's
# pnpm/action-setup), and the locked nixpkgs only decides which majors exist —
# `pnpm_NN`, or this throws. Within the major, nixpkgs' package is overridden
# onto the declared version when the two differ, so the devshell, the package
# build (and with it fetchPnpmDeps, whose store layout follows the pnpm that
# wrote it), CI and corepack in the container run one pnpm.
#
# pnpm 12 is a Rust build from the GitHub tag, so an override carries two
# hashes: the tag's source and the vendored crates. Both live in
# toolchains.json keyed by version; `.github/workflows/renovate-pins.yaml`
# derives them on renovate's branch.
let
  # corepack accepts `pnpm@X.Y.Z+sha512.…`; the hash suffix is corepack's and
  # says nothing about the version.
  declared = lib.head (
    lib.splitString "+" (lib.removePrefix "pnpm@" (lib.importJSON ../package.json).packageManager)
  );
  attr = "pnpm_${lib.versions.major declared}";

  pkg = lib.throwIf (!buildPackages ? ${attr}) ''
    package.json declares pnpm ${declared}, but nixpkgs has no ${attr}.
    A new pnpm major waits for nixpkgs to package it: hold packageManager on a
    major the locked nixpkgs has, or move the lock once it ships ${attr}.
  '' buildPackages.${attr};

  hashes =
    (lib.importJSON ./toolchains.json).pnpm.${declared} or (throw ''
      package.json declares pnpm ${declared}, but the locked nixpkgs has ${pkg.version},
      and nix/toolchains.json has no hashes for ${declared}.
      Add "src" (the v${declared} tag's source) and "cargo" (its vendored crates) under "pnpm".
    '');
in
if pkg.version == declared then
  pkg
else
  pkg.override {
    version = declared;
    srcHash = hashes.src;
    cargoHash = hashes.cargo;
  }
