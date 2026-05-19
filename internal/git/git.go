package git

import (
	"os"
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

// BranchName returns the current branch name, or "" when the caller
// is on detached HEAD pointing at a tag (a typical CI release deploy
// — those should hit the live URL, not a preview). In other detached
// scenarios falls back to GHA-supplied branch env vars.
func BranchName(dir string) string {
	c := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	c.Dir = dir
	out, err := c.Output()
	name := strings.TrimSpace(string(out))
	if err == nil && name != "" && name != "HEAD" {
		return name
	}
	// Detached HEAD. In GHA, GITHUB_REF_TYPE is "tag" or "branch";
	// tag → production deploy (no preview subdomain).
	if os.Getenv("GITHUB_REF_TYPE") == "tag" {
		return ""
	}
	if v := os.Getenv("GITHUB_HEAD_REF"); v != "" {
		return v
	}
	if v := os.Getenv("GITHUB_REF_NAME"); v != "" {
		return v
	}
	return ""
}
