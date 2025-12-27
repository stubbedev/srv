{ pkgs ? import <nixpkgs> { } }:

pkgs.buildGoModule {
  pname = "srv";
  version = "dev";

  src = ./.;

  vendorHash = "sha256-wwoPmFZGqFnPfWIDJ9H+g+d79F6FDB3gwFdrQLhlHHk=";

  ldflags = [
    "-s"
    "-w"
    "-X main.Version=dev"
    "-X main.Commit=none"
    "-X main.BuildDate=1970-01-01T00:00:00Z"
  ];

  meta = with pkgs.lib; {
    description = "A CLI tool for managing sites with Traefik reverse proxy";
    homepage = "https://github.com/stubbedev/srv";
    license = licenses.mit;
    maintainers = [ ];
    mainProgram = "srv";
  };
}
