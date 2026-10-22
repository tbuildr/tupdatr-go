{pkgs, ...}: {
  # Provides go, gopls and friends. No separate `go` in packages:
  # a second copy could shadow this one at a different version.
  languages.go = {
    enable = true;
    # optionally pin a specific package:
    # package = pkgs.go_1_25;
  };
}
