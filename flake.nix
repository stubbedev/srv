{
  description = "srv - Local development and production site manager with Traefik reverse proxy";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        version = self.shortRev or self.dirtyShortRev or "dev";
      in
      {
        packages = {
          srv = pkgs.buildGoModule {
            pname = "srv";
            version = version;
            src = self;

            vendorHash = "sha256-DxeOS7Pw8otCMTqWn/oCp5JyiuJyMoyZDZxwtIUfYeQ=";

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${version}"
              "-X main.Commit=${self.shortRev or self.dirtyShortRev or "dirty"}"
              "-X main.BuildDate=1970-01-01T00:00:00Z"
            ];

            # Build mkcert from the submodule before go build runs.
            # The binary is written to internal/mkcert/bin/mkcert where
            # the //go:embed directive expects it.
            preBuild = ''
              mkdir -p internal/mkcert/bin
              MKCERT_VERSION=$(cd third_party/mkcert && git describe --tags 2>/dev/null || echo "unknown")
              (cd third_party/mkcert && CGO_ENABLED=0 go build \
                -ldflags "-X main.Version=$MKCERT_VERSION" \
                -o ../../internal/mkcert/bin/mkcert .)

              # Generate version.go from the submodule's git tag.
              MKCERT_VERSION=$(cd third_party/mkcert && git describe --tags --abbrev=0 2>/dev/null || echo "unknown")
              cat > internal/mkcert/version.go <<EOF
              // Code generated during Nix build — do not edit.
              package mkcert

              // Version is the version of the embedded mkcert binary.
              const Version = "$MKCERT_VERSION"
              EOF
            '';

            meta = {
              description = "CLI tool for managing local development and production sites with Traefik reverse proxy";
              homepage = "https://github.com/stubbedev/srv";
              mainProgram = "srv";
            };
          };

          default = self.packages.${system}.srv;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            golangci-lint
          ];
        };
      }
    );
}
