package cmd

import (
	"fmt"
	"os"

	"github.com/mataki-dev/sandbar-cli/internal/client"
	"github.com/mataki-dev/sandbar-cli/internal/config"
)

type Globals struct {
	Token   string
	Version string
}

func (g *Globals) Client() *client.Client {
	token := g.Token
	if token == "" {
		var err error
		token, err = config.ResolveToken(config.GlobalConfigDir())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	}
	return client.NewFromEnv(token, g.Version)
}

func (g *Globals) WorkDir() string {
	dir, _ := os.Getwd()
	return dir
}

func (g *Globals) ProjectConfig() (*config.ProjectConfig, error) {
	return config.LoadProject(g.WorkDir())
}

func (g *Globals) SiteSlug() (string, error) {
	cfg, err := g.ProjectConfig()
	if err != nil {
		return "", err
	}
	if cfg.Site.Name == "" {
		return "", fmt.Errorf("no site name in .sandbar/config.toml. Run `sandbar init`")
	}
	return cfg.Site.Name, nil
}
