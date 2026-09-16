{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    devshell.url = "github:numtide/devshell";
    # Only ever used to evaluate the home-manager module in `checks`; nothing
    # in the package or the NixOS module depends on it.
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ inputs.devshell.flakeModule ];

      flake.nixosModules = {
        streamline = ./nix/module.nix;
        default = inputs.self.nixosModules.streamline;
      };

      flake.homeModules = {
        streamline = ./nix/hm-module.nix;
        default = inputs.self.homeModules.streamline;
      };

      flake.overlays.default = final: _prev: {
        streamline = final.callPackage ./nix/package.nix { };
      };

      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      perSystem =
        { pkgs, self', ... }:
        {
          packages = {
            streamline = pkgs.callPackage ./nix/package.nix { };
            default = self'.packages.streamline;
          };

          formatter = pkgs.nixfmt-tree;

          checks = {
            build = self'.packages.default;

            # Boots the NixOS module in a VM and drives the real service:
            # health probe, seeded admin login, SPA shell. This is what makes
            # the module a supported surface rather than an untested one.
            nixos = pkgs.testers.runNixOSTest {
              imports = [ ./nix/test.nix ];
              node.specialArgs.streamlineModule = inputs.self.nixosModules.streamline;
              # The nodes take the package from the overlay rather than from a
              # specialArg, so the wiring the README documents is the wiring
              # under test. Needs pkgsReadOnly off — a test node otherwise
              # inherits this `pkgs` and refuses its own `nixpkgs.overlays`.
              node.pkgsReadOnly = false;
              defaults.nixpkgs.overlays = [ inputs.self.overlays.default ];
            };

            # The home-manager module shares nix/options.nix and nix/common.nix
            # with the NixOS one, so building its activation package is enough
            # to catch the drift that matters — a unit that will not evaluate.
            home-manager =
              (inputs.home-manager.lib.homeManagerConfiguration {
                inherit pkgs;
                modules = [
                  inputs.self.homeModules.streamline
                  {
                    home = {
                      username = "streamline";
                      homeDirectory = "/home/streamline";
                      stateVersion = "26.05";
                    };
                    services.streamline = {
                      enable = true;
                      package = self'.packages.streamline;
                      settings.library.movie_path = "/home/streamline/media/movies";
                    };
                  }
                ];
              }).activationPackage;

            # Derived from the package rather than a runCommand of its own:
            # golangci-lint type-checks, so it needs the module cache and the
            # //go:embed assets that buildGoModule already arranges. A
            # standalone check had neither and only ever reported "could not
            # import entgo.io/ent" from a sandbox with no network.
            lint = self'.packages.streamline.overrideAttrs (old: {
              pname = "streamline-lint";
              buildPhase = ''
                ${old.preBuild}
                export HOME=$TMPDIR
                go tool golangci-lint run --timeout 15m
              '';
              installPhase = "touch $out";
              postInstall = "";
              dontFixup = true;
            });
          };

          devshells.default = {
            env = [
              {
                name = "CGO_ENABLED";
                value = 0;
              }
              {
                name = "CHROME_PATH";
                value = "${pkgs.chromium}/bin/chromium";
              }
              {
                name = "PLAYWRIGHT_BROWSERS_PATH";
                value = pkgs.playwright-driver.browsers;
              }
              {
                name = "PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS";
                value = true;
              }
              {
                name = "PLAYWRIGHT_NODEJS_PATH";
                value = "${pkgs.nodejs}/bin/node";
              }
              {
                name = "KUBECONFIG";
                eval = "\${PWD}/deploy/helm/streamline/kubeconfig.yaml";
              }
            ];
            packages = with pkgs; [
              act
              git-cliff
              go
              gopls
              nodejs
              # package.json pins pnpm@12, which nixpkgs does not carry yet.
              # The lockfile is v9, which 11 reads and writes identically, so
              # the only symptom is corepack's version warning.
              pnpm_11
              go-task
              openssl
              playwright-mcp
              pre-commit
              kind
              kubernetes-helm
              kubectl
              grype
              syft
              biome
              nixfmt
              # `task migrate:diff` writes the migration; rehashing atlas.sum
              # afterwards needs the CLI, which the ent library does not ship.
              atlas
              jq
              # headless: the e2e transcode spec drives ffmpeg/ffprobe, and
              # nothing here needs the X11/SDL outputs the full build pulls in.
              ffmpeg-headless
              sqlite
            ];
          };
        };
    };
}
