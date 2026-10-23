{ lib, ... }:
let
  password = "correct-horse-battery-staple";

  common =
    {
      pkgs,
      streamlineModule,
      ...
    }:
    {
      imports = [ streamlineModule ];

      virtualisation.memorySize = 2048;
      virtualisation.diskSize = 2048;

      environment.systemPackages = [ pkgs.jq ];

      # The seed admin password is a file the unit reads through
      # LoadCredential, which is also the shape the module documents for every
      # other secret — so the test exercises that path rather than a plain
      # string in the store.
      environment.etc."streamline-seed-password".text = password;

      services.streamline = {
        enable = true;
        # `package` is deliberately left at its default: the flake's overlay is
        # applied to every node, so this asserts the wiring the README tells
        # people to use rather than a package handed in past it.
        openFirewall = true;
        credentials.seed-password = "/etc/streamline-seed-password";
        settings = {
          server.host = "0.0.0.0";
          library = {
            movie_path = "/srv/media/movies";
            series_path = "/srv/media/series";
            download_path = "/srv/downloads";
          };
          auth = {
            registration_mode = "disabled";
            seed_admin = {
              email = "admin@example.com";
              password_file = "/run/credentials/streamline.service/seed-password";
            };
          };
        };
      };

      systemd.tmpfiles.settings."10-test-media" =
        lib.genAttrs
          [
            "/srv/media/movies"
            "/srv/media/series"
            "/srv/downloads"
          ]
          (_: {
            d = {
              user = "streamline";
              group = "streamline";
              mode = "0755";
            };
          });
    };
in
{
  name = "streamline";

  nodes.machine = common;

  # The second node is where every branch the first one leaves untaken gets
  # exercised: the store-config mode, the environment-file overlay, the render
  # device grant and a hand-added writable path.
  nodes.declarative = {
    imports = [ common ];

    environment.etc."streamline-env".text = ''
      STREAMLINE_SERVER__PORT=8081
    '';

    systemd.tmpfiles.settings."10-test-extra"."/srv/extra".d = {
      user = "streamline";
      group = "streamline";
      mode = "0755";
    };

    services.streamline = {
      mutableSettings = false;
      environmentFile = "/etc/streamline-env";
      hardwareAcceleration.enable = true;
      extraReadWritePaths = [ "/srv/extra" ];
    };
  };

  testScript = ''
    start_all()

    machine.wait_for_unit("streamline.service")
    machine.wait_for_open_port(8080)

    with subtest("health endpoint answers before auth"):
        out = machine.succeed("curl -sf http://127.0.0.1:8080/health")
        assert '"healthy"' in out, out

    with subtest("config was seeded into the state dir, not the store"):
        machine.succeed("test -f /var/lib/streamline/config.yaml")
        machine.succeed("grep -q 'movie_path' /var/lib/streamline/config.yaml")

    with subtest("ffprobe is wired to the nix ffmpeg"):
        machine.succeed("grep -qE 'path: /nix/store/.*ffmpeg.*/bin' /var/lib/streamline/config.yaml")

    # POST /auth/login answers 204 and sets streamline_session; the cookie's
    # value is the JWT, which /api/v1 also accepts as a bearer token.
    def login():
        machine.succeed(
            "curl -sf -c /tmp/jar -o /dev/null -X POST http://127.0.0.1:8080/auth/login"
            " -H 'Content-Type: application/json'"
            " -d '{\"email\":\"admin@example.com\",\"password\":\"${password}\"}'"
        )
        token = machine.succeed(
            "awk '$6 == \"streamline_session\" { print $7 }' /tmp/jar"
        ).strip()
        assert token, "login set no session cookie"
        return token

    with subtest("seeded admin can log in from its credential file"):
        token = login()
        machine.succeed(
            f"curl -sf http://127.0.0.1:8080/api/v1/system/info -H 'Authorization: Bearer {token}'"
            " | jq -e '.read_only == false'"
        )

    with subtest("bad credentials are refused"):
        machine.fail(
            "curl -sf -X POST http://127.0.0.1:8080/auth/login"
            " -H 'Content-Type: application/json'"
            " -d '{\"email\":\"admin@example.com\",\"password\":\"wrong\"}'"
        )

    with subtest("SPA shell is served for non-API paths"):
        machine.succeed("curl -sf http://127.0.0.1:8080/login | grep -qi 'doctype html'")

    with subtest("the database survives a restart"):
        machine.succeed("systemctl restart streamline.service")
        machine.wait_for_open_port(8080)
        login()

    with subtest("openFirewall reaches the port from another host"):
        declarative.wait_for_unit("network-online.target")
        declarative.succeed("curl -sf http://machine:8080/health")

    # The declarative node also carries environmentFile, hardwareAcceleration
    # and extraReadWritePaths, so 8081 — not 8080 — is the proof the env layer
    # won over the store config.
    declarative.wait_for_unit("streamline.service")
    declarative.wait_for_open_port(8081)

    with subtest("environmentFile overrides the store config"):
        declarative.succeed("curl -sf http://127.0.0.1:8081/health")
        declarative.fail("curl -sf --max-time 5 http://127.0.0.1:8080/health")

    with subtest("mutableSettings = false serves the store file read-only"):
        declarative.fail("test -e /var/lib/streamline/config.yaml")
        declarative.succeed(
            "systemctl show streamline.service -p ExecStart --value | grep -q -- '--config /nix/store'"
        )
        declarative.succeed(
            "curl -sf -c /tmp/jar -o /dev/null -X POST http://127.0.0.1:8081/auth/login"
            " -H 'Content-Type: application/json'"
            " -d '{\"email\":\"admin@example.com\",\"password\":\"${password}\"}'"
        )
        token = declarative.succeed(
            "awk '$6 == \"streamline_session\" { print $7 }' /tmp/jar"
        ).strip()
        declarative.succeed(
            f"curl -sf http://127.0.0.1:8081/api/v1/system/info -H 'Authorization: Bearer {token}'"
            " | jq -e '.read_only == true'"
        )

    with subtest("extraReadWritePaths adds to the derived set, never replaces it"):
        granted = declarative.succeed(
            "systemctl show streamline.service -p ReadWritePaths --value"
        )
        for path in ["/srv/extra", "/srv/media/movies", "/srv/downloads", "/var/lib/streamline"]:
            assert path in granted, f"{path} missing from ReadWritePaths: {granted}"

    with subtest("hardwareAcceleration opens the render device"):
        declarative.succeed(
            "systemctl show streamline.service -p DeviceAllow --value | grep -q /dev/dri"
        )
        declarative.succeed(
            "systemctl show streamline.service -p PrivateDevices --value | grep -q no"
        )
        declarative.succeed("id -nG streamline | grep -q render")
  '';
}
