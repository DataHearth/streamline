{
  lib,
  stdenv,
  buildPackages,
  fetchurl,
  autoPatchelfHook,
  installShellFiles,
  makeWrapper,
}:
# Same contract as nix/go.nix: package.json's packageManager decides the
# version (renovate bumps it from the npm registry, together with CI's
# pnpm/action-setup), and the locked nixpkgs only decides which majors exist —
# `pnpm_NN`, or this throws. When nixpkgs ships the declared version its
# package is used as-is, so the devshell, the package build (and with it
# fetchPnpmDeps, whose store layout follows the pnpm that runs it), CI and
# corepack in the container run one pnpm.
#
# Unlike Go and Node, a pnpm that differs from nixpkgs' is not rebuilt from
# source. pnpm 12+ is a Rust build whose recipe moves between patches — 12.10
# started generating a source file with esbuild before `cargo build`, which the
# pnpm_12 recipe written for 12.9.0 has no step for — so overriding the version
# breaks exactly when upstream reshapes its build. pnpm's own `@pnpm/exe`
# binary is what CI's pnpm/action-setup and corepack download anyway, so the
# override installs that one and patches it for the store. Its hashes are npm's
# `integrity` for each platform's tarball, kept in toolchains.json keyed by
# version; `.github/workflows/renovate-pins.yaml` reads them off the registry.
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

  system = stdenv.hostPlatform.system;
  platform =
    {
      x86_64-linux = "linux-x64";
      aarch64-linux = "linux-arm64";
    }
    .${system} or (throw "nix/pnpm.nix has no @pnpm/exe platform for ${system}");

  hash =
    (lib.importJSON ./toolchains.json).pnpm.${declared}.${system} or (throw ''
      package.json declares pnpm ${declared}, but the locked nixpkgs has ${pkg.version},
      and nix/toolchains.json has no hash for ${declared} on ${system}.
      Add npm's integrity under "pnpm": `curl -s https://registry.npmjs.org/@pnpm/exe.${platform}/${declared} | jq -r .dist.integrity`.
    '');
in
if pkg.version == declared then
  pkg
else
  stdenv.mkDerivation {
    pname = "pnpm";
    version = declared;

    src = fetchurl {
      url = "https://registry.npmjs.org/@pnpm/exe.${platform}/-/exe.${platform}-${declared}.tgz";
      inherit hash;
    };

    nativeBuildInputs = [
      autoPatchelfHook
      installShellFiles
      makeWrapper
    ];
    # libgcc_s; glibc comes with the stdenv.
    buildInputs = [ stdenv.cc.cc.lib ];

    dontConfigure = true;
    dontBuild = true;

    # The entry points and completions nixpkgs' own pnpm_NN installs.
    installPhase = ''
      runHook preInstall
      install -Dm755 pnpm "$out"/bin/pnpm
      makeWrapper "$out"/bin/pnpm "$out"/bin/pnpx --add-flag dlx
      ln -s pnpm "$out"/bin/pn
      ln -s pnpx "$out"/bin/pnx
      runHook postInstall
    '';

    # The hook patches in fixupPhase, after this runs; the binary has to be
    # executable here to print its completions.
    postInstall = lib.optionalString (stdenv.buildPlatform.canExecute stdenv.hostPlatform) ''
      autoPatchelf "$out"
      "$out"/bin/pnpm completion bash > pnpm.bash
      "$out"/bin/pnpm completion fish > pnpm.fish
      "$out"/bin/pnpm completion zsh > pnpm.zsh
      installShellCompletion pnpm.{bash,fish,zsh}
    '';

    # fetchPnpmDeps reads `nodejs-slim` off the pnpm it is handed.
    passthru = { inherit (pkg) majorVersion nodejs-slim; };

    meta = pkg.meta // {
      sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
    };
  }
