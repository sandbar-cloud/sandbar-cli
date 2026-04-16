package git

import (
	"os/exec"
	"strings"
)

// HeadMessage returns the HEAD commit message, or "" if not in a git repo.
func HeadMessage(dir string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// HeadSHA returns the full HEAD commit SHA, or "" if not in a git repo.
func HeadSHA(dir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// BranchName returns the current branch name, or "" if not in a git repo.
func BranchName(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
