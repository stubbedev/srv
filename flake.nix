{
  description = "srv - Local development site manager with Traefik reverse proxy";

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

            vendorHash = "sha256-G9qXDGhKs7mWO8jv52JGEjp0bNqy43NWLicCQ6vQvyY=";

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${version}"
              "-X main.Commit=${self.shortRev or self.dirtyShortRev or "dirty"}"
              "-X main.BuildDate=1970-01-01T00:00:00Z"
            ];

            # srv shells out to the system `mkcert` binary at runtime — propagate
            # it as a runtime dep so `nix run` users get a working CA tool.
            propagatedBuildInputs = [ pkgs.mkcert ];

            meta = {
              description = "CLI tool for managing local development sites with Traefik reverse proxy";
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
            mkcert
          ];
        };
      }
    );
}
