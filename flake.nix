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
          # nixpkgs' default `go` still trails the toolchain in go.mod, so the
          # builder is pinned to the same major the module declares.
          srv = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
            pname = "srv";
            version = version;
            src = self;

            # go-sum: 36672b4f5c71c2ca16552201e97491d0bb7ac2b6d55332f44569d78890c37efc
            vendorHash = "sha256-NgP9kD+qnDqhHgQGLSRCrpLWKNdJuQlrEHX3VJvGN0s=";

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${version}"
              "-X main.Commit=${self.shortRev or self.dirtyShortRev or "dirty"}"
              "-X main.BuildDate=1970-01-01T00:00:00Z"
            ];

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
            go_1_27
            gopls
            golangci-lint
          ];
        };
      }
    );
}
