{
  description = "A CLI tool for managing sites with Traefik reverse proxy";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "srv";
          version = self.rev or "dirty";

          src = ./.;

          vendorHash = "sha256-wwoPmFZGqFnPfWIDJ9H+g+d79F6FDB3gwFdrQLhlHHk=";

          ldflags = [
            "-s"
            "-w"
            "-X main.Version=${self.rev or "dev"}"
            "-X main.Commit=${self.rev or "none"}"
            "-X main.BuildDate=1970-01-01T00:00:00Z"
          ];

          meta = with pkgs.lib; {
            description = "A CLI tool for managing sites with Traefik reverse proxy";
            homepage = "https://github.com/stubbedev/srv";
            license = licenses.mit;
            maintainers = [ ];
            mainProgram = "srv";
          };
        };

        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/srv";
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gotools
            gopls
            go-tools
            docker
          ];

          shellHook = ''
            echo "srv development environment"
            echo "Go version: $(go version)"
          '';
        };
      }
    );
}
