{ self, ... }:
{
  perSystem =
    {
      lib,
      pkgs,
      self',
      ...
    }:
    let
      version = self.shortRev or self.dirtyShortRev or "dev";
    in
    {
      packages = {
        # nixpkgs' default `go` still trails the toolchain in go.mod, so the
        # builder is pinned to the same major the module declares.
        srv = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
          pname = "srv";
          inherit version;
          src = lib.fileset.toSource {
            root = ../.;
            fileset = lib.fileset.difference (lib.fileset.gitTracked ../.) (
              lib.fileset.unions [
                ../flake.nix
                ../flake.lock
                ../nix
              ]
            );
          };

          # go-sum: 5633de83a244baa72b55ae410eb6671235bf073220b7dc6d9882b806797fe01f
          vendorHash = "sha256-I8cARWxu/0eM22haMwKZPFBYYOa3q/gW7jRmeqv0iHg=";

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

        default = self'.packages.srv;
      };
    };
}
