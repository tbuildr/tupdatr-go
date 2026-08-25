package main

// Action describes one update task.
//
// Exactly one of Shell/Command or Argv is used:
//
//   - Argv (preferred): executed directly, no shell involved, so there is
//     nothing for a shell to reinterpret.
//   - Command: a fixed compile-time literal run via `sh -c`; used only where
//     pipes, loops, && or command substitution are genuinely required.
type Action struct {
	Key         string   // long-flag name, e.g. "bootc"
	Label       string   // menu display text
	Description string   // one-line explanation shown in menu and --help
	Shell       bool     // true => run Command via sh -c, false => exec Argv
	Argv        []string //
	Command     string   //
}

var actions = []Action{
	{
		Key:         "devenv",
		Label:       "Devenv packages",
		Description: "Find every devenv.nix under $HOME and run devenv update in it",
		Shell:       true,
		Command: `find "$HOME" \( -name .cache -o -name .git -o -name .var` +
			` -o -name .mozilla -o -name .steam -o -name .local` +
			` -o -name node_modules -o -name result -o -name templates \)` +
			` -prune -o -type f -name devenv.nix -print | while read -r f; do` +
			` d=$(dirname "$f"); echo "== $d =="; (cd "$d" && devenv update); done`,
	},
	{
		Key:         "distrobox",
		Label:       "Distrobox containers",
		Description: "Upgrade all distrobox containers",
		Argv:        []string{"distrobox-upgrade", "--all"},
	},
	{
		Key:         "firmware",
		Label:       "Firmware (fwupd)",
		Description: "Refresh metadata and apply available firmware updates",
		Shell:       true,
		Command:     "fwupdmgr refresh && fwupdmgr update -y",
	},
	{
		Key:         "flatpak",
		Label:       "Flatpak apps",
		Description: "Update all installed flatpak apps",
		Argv:        []string{"flatpak", "update", "-y"},
	},
	{
		Key:         "flakes",
		Label:       "Nix flakes (all)",
		Description: "Find every flake.nix under $HOME and update it",
		Shell:       true,
		Command: `find "$HOME" \( -name .cache -o -name .git -o -name .var` +
			` -o -name .mozilla -o -name .steam -o -name .local` +
			` -o -name node_modules -o -name result -o -name templates \)` +
			` -prune -o -type f -name flake.nix -print | while read -r f; do` +
			` d=$(dirname "$f"); echo "== $d ==";` +
			` nix --extra-experimental-features 'nix-command flakes' flake update --flake "$d"; done`,
	},
	{
		Key:         "podman",
		Label:       "Podman auto-update",
		Description: "Pull + recreate labeled podman containers",
		Argv:        []string{"podman", "auto-update"},
	},
	{
		Key:         "system",
		Label:       "System image (rpm-ostree)",
		Description: "Apply pending rpm-ostree upgrade",
		Argv:        []string{"rpm-ostree", "upgrade"},
	},
}
