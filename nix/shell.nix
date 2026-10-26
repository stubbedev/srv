{
  perSystem =
    { pkgs, self', ... }:
    let
      mkdocs = pkgs.python3.withPackages (ps: [ ps.mkdocs-material ]);
    in
    {
      devShells.default = pkgs.mkShell {
        inputsFrom = [ self'.packages.default ];

        packages = [
          mkdocs
        ]
        ++ (with pkgs; [
          gopls
          golangci-lint
        ]);
      };

      # The docs site build: regenerates the reference pages (go) and
      # renders them with MkDocs Material. Shared with the default shell so
      # `just docs-serve` matches what Pages publishes.
      devShells.docs = pkgs.mkShell {
        inputsFrom = [ self'.packages.default ];
        packages = [
          mkdocs
          pkgs.just
        ];
      };
    };
}
