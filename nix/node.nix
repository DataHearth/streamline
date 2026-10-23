{
  lib,
  buildPackages,
  fetchurl,
}:
# Same contract as nix/go.nix: package.json's engines.node decides the version
# (renovate bumps it from nodejs.org's release list), and the locked nixpkgs
# only decides which majors exist — `nodejs_NN`, or this throws. Within the
# major, nixpkgs' package is overridden onto the declared version when the two
# differ, so setup-node, the container base image and the devshell agree.
#
# `>=` is for anyone building without the devshell — they owe the project at
# least this version and may be ahead of it. The devshell owes it exactly, so
# the floor is what this resolves to. The operator has to come off before the
# string is parsed: `lib.versions.major ">=24.20.0"` is `">=24"`, which would
# look up an attribute that does not exist.
#
# `nodejs_NN` is a symlinkJoin over `nodejs-slim_NN` (node itself) and its npm
# and corepack outputs, so the override goes on the slim build and the wrapper
# is rebuilt around it. nixpkgs computes a few configure flags from the version
# it was written for; within one major those agree, which is the only range
# this overrides across. An overridden node is compiled from source — the
# homelab builder keeps it, so CI pays that once per version.
let
  declared = lib.removePrefix ">=" (lib.importJSON ../package.json).engines.node;
  major = lib.versions.major declared;
  attr = "nodejs_${major}";

  pkg = lib.throwIf (!buildPackages ? ${attr}) ''
    package.json declares Node ${declared}, but nixpkgs has no ${attr}.
    A new Node major waits for nixpkgs to package it: hold engines.node on a
    major the locked nixpkgs has, or move the lock once it ships ${attr}.
  '' buildPackages.${attr};

  hash =
    (lib.importJSON ./toolchains.json).node.${declared} or (throw ''
      package.json declares Node ${declared}, but the locked nixpkgs has ${pkg.version},
      and nix/toolchains.json has no source hash for ${declared}.
      Add it under "node": `nix store prefetch-file https://nodejs.org/dist/v${declared}/node-v${declared}.tar.xz`.
    '');
in
if pkg.version == declared then
  pkg
else
  pkg.override {
    nodejs-slim = buildPackages."nodejs-slim_${major}".overrideAttrs {
      version = declared;
      src = fetchurl {
        url = "https://nodejs.org/dist/v${declared}/node-v${declared}.tar.xz";
        inherit hash;
      };
    };
  }
