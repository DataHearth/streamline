{
  lib,
  stdenvNoCC,
  buildGoModule,
  callPackage,
  fetchPnpmDeps,
  pnpmConfigHook,
  pnpm,
  nix-update-script,
  version ? "3.1.0",
}:
let
  go = callPackage ./go.nix { };
  nodejs = callPackage ./node.nix { };
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
      inherit (finalAttrs) pname version src;
      fetcherVersion = 4;
      hash = "sha256-0Hv/nmdcQx7nFkMnTnpgAAahvONhmOgL+ffh/RFqKzM=";
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

  vendorHash = "sha256-qcN6b+2ewk4GTLYFW3Vc3nEhXJphvVOww7bt/SS1Dwk=";

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
    inherit frontend;
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
