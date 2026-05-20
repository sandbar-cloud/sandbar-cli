package cmd

import (
	"fmt"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
)

// SyncCmd applies the declarative bits of .sandbar/config.toml to the
// server without uploading or activating a deploy. Same reconcile
// helpers `sandbar deploy` runs after a successful production deploy —
// site fields, domains, trusts, preview expiry.
//
// Useful when you've edited config and want server state caught up
// without rebuilding/redeploying, or when bootstrapping a brand-new
// trust before the first CI deploy can auth.
type SyncCmd struct{}

func (cmd *SyncCmd) Run(globals *Globals) error {
	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	slug := cfg.Site.EffectiveSlug()
	if slug == "" {
		return fmt.Errorf("no site slug in .sandbar/config.toml. Run `sandbar init`")
	}
	return cmd.RunWith(globals.Client(), slug, cfg)
}

// RunWith runs the sync against an explicit client and resolved
// config. Exposed separately so tests can inject a mock server
// without driving Globals through env.
func (cmd *SyncCmd) RunWith(c *client.Client, slug string, cfg *config.ProjectConfig) error {
	reconcileSite(c, slug, cfg.Site)
	if cfg.Domains != nil {
		reconcileDomains(c, slug, cfg.Domains)
	}
	if cfg.Trusts != nil {
		reconcileTrusts(c, slug, cfg.Trusts)
	}
	if cfg.Preview.DefaultExpiry != "" {
		reconcilePreviewExpiry(c, slug, cfg.Preview.DefaultExpiry)
	}
	return nil
}
