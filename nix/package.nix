{
  lib,
  stdenvNoCC,
  buildGoModule,
  callPackage,
  fetchPnpmDeps,
  pnpmConfigHook,
  nix-update-script,
  version ? "3.2.0",
}:
let
  go = callPackage ./go.nix { };
  nodejs = callPackage ./node.nix { };
  # Not the `pnpm` callPackage would hand in: that is whatever nixpkgs calls
  # its default, and fetchPnpmDeps lays the store out the way the pnpm that
  # runs it does — so it has to be the one package.json declares.
  pnpm = callPackage ./pnpm.nix { };

  # A fixed-output derivation's store path is its name and its declared hash,
  # nothing else — so while the name stays put, a hash left stale by a lockfile
  # or toolchain change still resolves to the old output wherever that path is
  # already in a store, and nothing re-fetches to notice. The homelab builder
  # did exactly that for pnpmDeps after the pnpm 12.11.2 bump: CI passed a hash
  # a clean machine would have rejected. Naming each one after what decides its
  # content moves the path whenever that does, so the fetch runs and the hash
  # is checked for real.
  inputsId =
    files: extra:
    builtins.substring 0 12 (
      builtins.hashString "sha256" (
        lib.concatStringsSep "\n" (map (builtins.hashFile "sha256") files ++ extra)
      )
    );

  # The build tree, minus everything the frontend build regenerates. Those
  # outputs are gitignored, so they are absent in CI and present on a
  # developer's machine — including them would make the source hash depend on
  # whether someone had run `task build:js` locally.
  src = lib.fileset.toSource {
    root = ../.;
    fileset =
      lib.fileset.difference
        (lib.fileset.unions [
          ../api
          ../cmd
          ../ent
          ../internal
          ../web
          # Only the `lint` check reads this; it rides along so that check does not
          # need a second source tree. `e2e/` deliberately does not — its rod/
          # chromium test deps are not in the vendored module set, so linting it
          # here can only fail on imports. `task lint:go` is what covers it.
          ../.golangci.yml
          ../go.mod
          ../go.sum
          ../package.json
          ../pnpm-lock.yaml
          ../pnpm-workspace.yaml
          ../routify.config.js
        ])
        (
          lib.fileset.unions (
            map lib.fileset.maybeMissing [
              ../web/app/.routify
              ../web/app/lib/paraglide
              ../web/static/css/docs.min.css
              ../web/static/css/style.css
              ../web/static/dist
              ../web/static/js/bundle.min.js
              ../web/static/js/docs.min.js
            ]
          )
        );
  };

  frontend = stdenvNoCC.mkDerivation (finalAttrs: {
    pname = "streamline-frontend";
    inherit version src;

    nativeBuildInputs = [
      nodejs
      pnpm
      pnpmConfigHook
    ];

    pnpmDeps = fetchPnpmDeps {
      # Its output is named `${pname}-pnpm-deps`.
      pname = "${finalAttrs.pname}-${
        inputsId [ ../pnpm-lock.yaml ../pnpm-workspace.yaml ] [ pnpm.version ]
      }";
      inherit (finalAttrs) version src;
      inherit pnpm;
      fetcherVersion = 4;
      hash = "sha256-RZd8Suj4qyVfrVeXwn3cYoAteTyHBmkb5XCEbDAhGMA=";
    };

    # Mirrors `task build:js` + `task build:css`. Keep the two in step: the Go
    # build embeds exactly these outputs and a missing one fails //go:embed
    # with "no matching files found", which reads like broken code.
    buildPhase = ''
      runHook preBuild

      mkdir -p bundle web/static/dist
      pnpm exec esbuild web/static/js/docs.js --bundle --minify \
        --outdir=bundle --entry-names='[name].min'
      mv bundle/docs.min.js web/static/js/docs.min.js
      mv bundle/docs.min.css web/static/css/docs.min.css

      pnpm exec paraglide-js compile --project ./web/app/project.inlang \
        --outdir ./web/app/lib/paraglide --is-server false \
        --emit-ts-declarations \
        --strategy localStorage preferredLanguage baseLocale
      pnpm exec routify build
      node web/app/esbuild.config.mjs

      pnpm exec tailwindcss -i web/static/css/input.css \
        -o web/static/css/style.css --minify

      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      mkdir -p $out/css $out/js
      cp web/static/css/style.css web/static/css/docs.min.css $out/css/
      cp web/static/js/docs.min.js $out/js/
      cp -r web/static/dist $out/dist
      runHook postInstall
    '';
  });
in
(buildGoModule.override { inherit go; }) {
  pname = "streamline";
  inherit version src;

  vendorHash = "sha256-yDTaduStH8nmV6p7rJ23x0GMW9Xz7w0o2ptShQHACSU=";
  # Named by what decides the vendored set rather than by the app version, for
  # the reason inputsId gives. Fixed rather than derived from pname, so the lint
  # check's overrideAttrs shares this fetch instead of repeating it.
  overrideModAttrs = {
    name = "streamline-go-modules-${inputsId [ ../go.mod ../go.sum ] [ go.version ]}";
  };

  subPackages = [ "cmd" ];

  env.CGO_ENABLED = 0;

  preBuild = ''
    cp ${frontend}/css/style.css web/static/css/style.css
    cp ${frontend}/css/docs.min.css web/static/css/docs.min.css
    cp ${frontend}/js/docs.min.js web/static/js/docs.min.js
    mkdir -p web/static/dist
    cp ${frontend}/dist/* web/static/dist/
  '';

  ldflags = [
    "-s"
    "-w"
    "-X github.com/datahearth/streamline/internal/buildinfo.Version=${version}"
    "-X github.com/datahearth/streamline/internal/buildinfo.Commit=nix"
    "-X github.com/datahearth/streamline/internal/buildinfo.Date=unknown"
  ];

  # The suites are Ginkgo-driven and several reach the filesystem and a real
  # ffprobe; they run through `task test`, not as part of packaging.
  doCheck = false;

  postInstall = ''
    mv $out/bin/cmd $out/bin/streamline
  '';

  passthru = {
    # The pins, beside `go` (which buildGoModule already exposes), so
    # renovate-pins.yaml can derive a toolchain hash from `.#streamline.pnpm`
    # rather than an impure expression re-importing nix/pnpm.nix.
    inherit frontend nodejs pnpm;
    updateScript = nix-update-script { };
  };

  meta = {
    description = "Unified media management platform replacing the *arr stack";
    homepage = "https://github.com/DataHearth/streamline";
    license = lib.licenses.gpl3Only;
    mainProgram = "streamline";
    platforms = lib.platforms.linux ++ lib.platforms.darwin;
  };
}
