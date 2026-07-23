{
  description = "Declarative configuration for Multica workspaces";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-darwin"
        "x86_64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          package = pkgs.buildGoModule {
            pname = "multica-declarative";
            version = "0.4.0";

            src = self;
            vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";

            subPackages = [ "cmd/multica-declarative" ];

            meta = {
              description = "Declarative configuration for Multica workspaces";
              homepage = "https://github.com/Tr0sT/multica-declarative";
              license = nixpkgs.lib.licenses.mit;
              mainProgram = "multica-declarative";
            };
          };
        in
        {
          default = package;
          multica-declarative = package;
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/multica-declarative";
          meta.description = "Manage Multica workspaces declaratively";
        };
      });
    };
}
