// Package hcombin resolves executable paths without breaking tools that
// dispatch by the basename used to invoke them.
package hcombin

import (
	"os"
	"path/filepath"
)

// ResolveExecPath returns an absolute executable path and reports whether it
// preserved an argv0-dispatch symlink. Ordinary hcom symlinks resolve to their
// target; a symlink whose target basename is not hcom stays at its invoked path
// so executing it still presents argv[0] as hcom to the dispatcher.
func ResolveExecPath(path string) (resolved string, argv0Dispatch bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	target, evalErr := filepath.EvalSymlinks(abs)
	if evalErr != nil {
		return abs, false, nil
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return "", false, err
	}
	if st.Mode()&os.ModeSymlink != 0 && filepath.Base(target) != "hcom" {
		return abs, true, nil
	}
	return target, false, nil
}
