// Package containername derives safe, branch-scoped Docker container names for
// test harnesses so concurrent worktrees sharing one Colima/Docker daemon never
// collide on shared containers, networks or volumes.
package containername

import (
	"os/exec"
	"regexp"
	"strings"
)

var unsafeChars = regexp.MustCompile(`[^a-z0-9-]+`)

// Prefix returns a Docker-safe prefix scoped to the current git branch:
// "prism-feature-ban" for branch "feature/ban" (the established program
// convention is prism-<normalized-branch>-<suite>-<suffix>). It falls back to
// "prism" when the branch cannot be resolved or normalizes to an empty value,
// so the resulting names stay Docker-safe in every environment.
func Prefix() string {
	output, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "prism"
	}
	branch := strings.ToLower(strings.TrimSpace(string(output)))
	if branch == "" || branch == "head" {
		return "prism"
	}
	safe := unsafeChars.ReplaceAllString(branch, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "prism"
	}
	return "prism-" + safe
}
