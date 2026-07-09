package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type execResult struct {
	ExitCode int
}

type deps struct {
	env          func(string) string
	cwd          func() (string, error)
	exec         func(name string, args []string, dir string, stdin io.Reader, stdout, stderr io.Writer) execResult
	git          func(args []string, dir string) ([]byte, error)
	clock        func() time.Time
	stdout       io.Writer
	stderr       io.Writer
	missionsRepo string
}

func newDeps(stdout, stderr io.Writer) deps {
	env := os.Getenv
	return deps{
		env:          env,
		cwd:          os.Getwd,
		exec:         runExec,
		git:          runGit,
		clock:        time.Now,
		stdout:       stdout,
		stderr:       stderr,
		missionsRepo: env("MISSIONS_REPO"),
	}
}

func runExec(name string, args []string, dir string, stdin io.Reader, stdout, stderr io.Writer) execResult {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return execResult{ExitCode: 0}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return execResult{ExitCode: exitErr.ExitCode()}
	}
	return execResult{ExitCode: 1}
}

func runGit(args []string, dir string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}
