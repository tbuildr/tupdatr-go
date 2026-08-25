package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	fs := flag.NewFlagSet(argv[0], flag.ContinueOnError)
	requested := make([]bool, len(actions))
	for i := range actions {
		fs.BoolVar(&requested[i], actions[i].Key, false, actions[i].Description)
	}
	doAll := fs.Bool("all", false, "run every update action")
	dryRun := fs.Bool("dry-run", false, "print commands instead of running them")
	doList := fs.Bool("list", false, "list available actions and exit")
	var help bool
	fs.BoolVar(&help, "help", false, "show this help")
	fs.BoolVar(&help, "h", false, "show this help")
	// GNU getopt_long permutes argv and this program ignores any leftover
	// positional words (argv[optind..]), so flags work anywhere and stray
	// non-option tokens are skipped, matching main.c.
	var flagArgs []string
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		usage()
		return 1
	}

	if help {
		usage()
		return 0
	}
	if *doList {
		listActions()
		return 0
	}

	if *doAll {
		return runSequence(allIndexes(), *dryRun)
	}
	var picked []int
	for i, want := range requested {
		if want {
			picked = append(picked, i)
		}
	}
	if len(picked) > 0 {
		return runSequence(picked, *dryRun)
	}

	code, err := runTUI(*dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

// runSequence mirrors main.c's --all / explicit-flag paths: run each action
// in table order, keep the last nonzero status as the process exit code.
func runSequence(order []int, dryRun bool) int {
	overall := 0
	ctx := context.Background()
	for _, i := range order {
		if st := RunAction(ctx, actions[i], dryRun, nil); st != 0 {
			overall = st
		}
	}
	return overall
}

func allIndexes() []int {
	idxs := make([]int, len(actions))
	for i := range actions {
		idxs[i] = i
	}
	return idxs
}

func listActions() {
	for _, a := range actions {
		fmt.Printf("%-12s %s\n", a.Key, a.Description)
	}
}

func usage() {
	fmt.Printf("Usage: %s [options]\n\n", os.Args[0])
	fmt.Println("With no options, opens the interactive menu.")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --all              run every update action")
	for _, a := range actions {
		fmt.Printf("  --%-16s %s\n", a.Key, a.Description)
	}
	fmt.Println("  --dry-run          print commands instead of running them")
	fmt.Println("  --list             list available actions and exit")
	fmt.Println("  -h, --help         show this help")
}
