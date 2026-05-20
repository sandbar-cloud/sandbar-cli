package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandbar-cloud/sandbar-cli/internal/config"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type InitCmd struct {
	Name string `help:"Site name." short:"n"`
	Dir  string `help:"Build output directory." short:"d"`
	Yes  bool   `help:"Accept all defaults." short:"y"`
}

// Run scaffolds .sandbar/config.toml locally. No API call, no auth needed.
// The site is created on first deploy.
func (cmd *InitCmd) Run(globals *Globals) error {
	workDir := globals.WorkDir()
	configPath := filepath.Join(workDir, ".sandbar", "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf(".sandbar/config.toml already exists. Remove it to reinitialize")
	}

	reader := bufio.NewReader(os.Stdin)

	name := cmd.Name
	if name == "" {
		defaultName := filepath.Base(workDir)
		if cmd.Yes {
			name = defaultName
		} else {
			fmt.Printf("  Site name [%s]: ", defaultName)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" {
				name = defaultName
			} else {
				name = input
			}
		}
	}

	detection := detectBuild(workDir)
	buildDir := cmd.Dir
	framework := detection.framework
	if buildDir == "" {
		if cmd.Yes {
			buildDir = detection.dir
		} else {
			fmt.Printf("  Build directory [%s]: ", detection.dir)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" {
				buildDir = detection.dir
			} else {
				buildDir = input
				framework = ""
			}
		}
	} else {
		framework = ""
	}

	cfg := &config.ProjectConfig{
		Site: config.SiteConfig{
			Slug:             name,
			ProductionBranch: "main",
			BuildDir:         buildDir,
			Framework:        framework,
		},
		Deploy: config.DeployConfig{
			AutoActivate:   true,
			MessageFromGit: true,
		},
		Preview: config.PreviewConfig{
			DefaultExpiry: "7d",
		},
	}

	if err := config.WriteProject(workDir, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\n  Created .sandbar/config.toml\n")
	fmt.Printf("  Site:  %s\n", output.Bold.Render(name))
	fmt.Printf("  Build: %s\n", buildDir)
	fmt.Printf("\n  Next: %s\n\n", output.Dim.Render("sandbar deploy"))
	return nil
}

type buildDetection struct {
	dir       string
	framework string
}

func detectBuild(dir string) buildDetection {
	// Map build dirs to frameworks
	candidates := []struct {
		dir       string
		framework string
	}{
		{"dist", "astro"},
		{"public", "hugo"},
		{"build", "cra"},
		{"out", "next"},
	}
	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(dir, c.dir)); err == nil && info.IsDir() {
			return buildDetection{dir: c.dir, framework: c.framework}
		}
	}
	return buildDetection{dir: "dist", framework: ""}
}
