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

            # go-sum: db4251b8ba8a3642ccdf12bd5a292a75fea3647f1ebb901291368a94f21314b8
            vendorHash = "sha256-vDOKYEyDK7TapeYCIAhJf2UMgTQAVpqL3Nuv4ti11mo=";

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
            go_1_27
            gopls
            golangci-lint
            mkcert
          ];
        };
      }
    );
}
