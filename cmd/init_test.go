package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mataki-dev/sandbar-cli/internal/config"
)

func TestInitCmd_CreatesConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "dist"), 0o755)

	// Fake working directory
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := InitCmd{Name: "my-site", Dir: "dist", Yes: true}
	err := cmd.Run(&Globals{Version: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if cfg.Site.Name != "my-site" {
		t.Errorf("site name = %q, want %q", cfg.Site.Name, "my-site")
	}
	if cfg.Site.BuildDir != "dist" {
		t.Errorf("build_dir = %q, want %q", cfg.Site.BuildDir, "dist")
	}
}

func TestInitCmd_AbortIfConfigExists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sandbar"), 0o755)
	os.WriteFile(filepath.Join(dir, ".sandbar", "config.toml"), []byte("[site]\nname=\"x\""), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := InitCmd{Name: "my-site", Yes: true}
	err := cmd.Run(&Globals{Version: "test"})
	if err == nil {
		t.Fatal("expected error for existing config, got nil")
	}
}
