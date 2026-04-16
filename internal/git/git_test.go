package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mataki-dev/sandbar-cli/internal/git"
)

var gitEnv = []string{
	"GIT_AUTHOR_NAME=test",
	"GIT_AUTHOR_EMAIL=test@test.com",
	"GIT_COMMITTER_NAME=test",
	"GIT_COMMITTER_EMAIL=test@test.com",
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), gitEnv...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	// Create a file and commit
	f := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	cmd := exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), gitEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit failed: %s", out)
	}

	return dir
}

func TestHeadMessage_InGitRepo(t *testing.T) {
	dir := initRepo(t)
	msg := git.HeadMessage(dir)
	if msg != "initial commit" {
		t.Errorf("expected 'initial commit', got %q", msg)
	}
}

func TestHeadMessage_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	msg := git.HeadMessage(dir)
	if msg != "" {
		t.Errorf("expected empty string, got %q", msg)
	}
}

func TestBranchName_InGitRepo(t *testing.T) {
	dir := initRepo(t)
	branch := git.BranchName(dir)
	if branch != "main" {
		t.Errorf("expected 'main', got %q", branch)
	}
}

func TestBranchName_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	branch := git.BranchName(dir)
	if branch != "" {
		t.Errorf("expected empty string, got %q", branch)
	}
}
