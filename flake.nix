{
  description = "srv - Local development and production site manager with Traefik reverse proxy";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    mkcert = {
      url = "github:FiloSottile/mkcert/1c1dc4ed27ed5936046b6398d39cab4d657a2d8e";
      flake = false;
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      mkcert,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        version = self.shortRev or self.dirtyShortRev or "dev";

        # Build mkcert from the submodule as a separate derivation.
        # This runs in its own sandbox with its own vendorHash, completely
        # independent of srv's dependency fetch — so our preBuild hook
        # is never inherited by the goModules fixed-output derivation.
        mkcertBin = pkgs.buildGoModule {
          pname = "mkcert";
          version = mkcert.shortRev or "unknown";
          src = mkcert;
          vendorHash = "sha256-DdA7s+N5S1ivwUgZ+M2W/HCp/7neeoqRQL0umn3m6Do=";
          env.CGO_ENABLED = "0";
          ldflags = [ "-X main.Version=${mkcert.shortRev or "unknown"}" ];
          meta.mainProgram = "mkcert";
        };

      in
      {
        packages = {
          srv = pkgs.buildGoModule {
            pname = "srv";
            version = version;
            src = self;

            vendorHash = "sha256-4ST7U4CAfwHNlFiCjr4ldTamUANwu1iBF5hQlUZuVNw=";

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${version}"
              "-X main.Commit=${self.shortRev or self.dirtyShortRev or "dirty"}"
              "-X main.BuildDate=1970-01-01T00:00:00Z"
            ];

            # Copy the pre-built mkcert binary into place before go build runs.
            # This is NOT inherited by the goModules derivation (only preBuild is),
            # so it runs only in the main build sandbox where the binary is available.
            preConfigure = ''
              mkdir -p internal/mkcert/bin
              cp ${mkcertBin}/bin/mkcert internal/mkcert/bin/mkcert

              cat > internal/mkcert/version.go <<'EOF'
              // Code generated during Nix build — do not edit.
              package mkcert

              const Version = "submodule"
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
