{
  perSystem =
    { pkgs, self', ... }:
    {
      devShells.default = pkgs.mkShell {
        inputsFrom = [ self'.packages.default ];

        packages = with pkgs; [
          gopls
          golangci-lint
        ];
      };
    };
}
