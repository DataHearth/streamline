{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    devshell.url = "github:numtide/devshell";
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ inputs.devshell.flakeModule ];

      systems = [ "x86_64-linux" ];
      perSystem =
        { pkgs, self', ... }:
        {
          packages.default = pkgs.buildGoModule {
            pname = "streamline";
            version = "0.1.0";
            src = ./.;
            vendorHash = null;
          };

          checks = {
            build = self'.packages.default;
            lint =
              pkgs.runCommandLocal "check-golangci-lint"
                {
                  nativeBuildInputs = with pkgs; [
                    go
                    golangci-lint
                  ];
                }
                ''
                  export HOME=$TMPDIR
                  export GOPATH=$TMPDIR/go
                  export GOCACHE=$TMPDIR/go-cache
                  cp -r ${./.} src
                  chmod -R u+w src
                  cd src
                  golangci-lint run
                  touch $out
                '';
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
              pnpm
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
              djlint
              sqlite
            ];
          };
        };
    };
}
