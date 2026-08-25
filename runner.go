package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Exit status used when the program itself cannot be spawned; mirrors the
// C version's execvp failure path (_exit(127)).
const statusSpawnFailed = 127

// LineSink receives each captured output line as it is produced.
// It may be nil, in which case the child writes straight to our stdout/stderr.
type LineSink func(string)

// RunAction executes one action and returns the command's exit status.
// If dryRun is true it prints the command instead of running it (same text
// as the C version) and returns 0. When sink is non-nil, combined output is
// streamed to it line by line instead of going to the terminal directly.
func RunAction(ctx context.Context, a Action, dryRun bool, sink LineSink) int {
	if dryRun {
		cmdText := a.Command
		if !a.Shell {
			cmdText = strings.Join(a.Argv, " ")
		}
		emit(sink, fmt.Sprintf("[dry-run] %s: %s", a.Label, cmdText))
		return 0
	}

	emit(sink, fmt.Sprintf("== %s ==", a.Label))

	var cmd *exec.Cmd
	if a.Shell {
		cmd = exec.CommandContext(ctx, "sh", "-c", a.Command)
	} else {
		cmd = exec.CommandContext(ctx, a.Argv[0], a.Argv[1:]...)
	}

	if sink == nil {
		// Plain CLI mode: let the child inherit the terminal, exactly like
		// the fork()+execvp() path in the C version.
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start %s: %v\n", cmd.Path, err)
			return statusSpawnFailed
		}
		return waitStatus(cmd.Wait())
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return statusSpawnFailed
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return statusSpawnFailed
	}
	if err := cmd.Start(); err != nil {
		emit(sink, fmt.Sprintf("failed to start %s: %v", cmd.Path, err))
		return statusSpawnFailed
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pump(stdout, sink) }()
	go func() { defer wg.Done(); pump(stderr, sink) }()
	wg.Wait()
	err = cmd.Wait()

	// Surface abnormal termination the way the C version reported signals.
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() < 0 {
		emit(sink, fmt.Sprintf("%s terminated by signal %d", firstOf(a), -ee.ExitCode()))
	}
	return waitStatus(err)
}

func emit(sink LineSink, line string) {
	if sink != nil {
		sink(line)
		return
	}
	fmt.Println(line)
}

func pump(r io.Reader, sink LineSink) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		sink(sc.Text())
	}
}

func waitStatus(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode() // -1 when signaled, matching WIFSIGNALED handling
	}
	return statusSpawnFailed
}

func firstOf(a Action) string {
	if a.Shell {
		return "sh"
	}
	return a.Argv[0]
}
