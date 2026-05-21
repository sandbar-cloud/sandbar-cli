package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
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
	start := time.Now()
	fmt.Printf("Syncing %s\n", output.Bold.Render(slug))

	syncSection("site metadata", true, func() {
		reconcileSite(c, slug, cfg.Site)
	})
	syncSection("domains", cfg.Domains != nil, func() {
		reconcileDomains(c, slug, cfg.Domains)
	})
	syncSection("OIDC trusts", cfg.Trusts != nil, func() {
		reconcileTrusts(c, slug, cfg.Trusts)
	})
	syncSection("preview expiry", cfg.Preview.DefaultExpiry != "", func() {
		reconcilePreviewExpiry(c, slug, cfg.Preview.DefaultExpiry)
	})

	fmt.Printf("\nDone in %s\n", time.Since(start).Round(100*time.Millisecond))
	return nil
}

// syncSection runs body under a spinner with the given label. When
// active is false (the corresponding config block is absent), the
// section is marked skipped instead. Any diff lines the reconciler
// prints to stdout appear above the spinner's stop line because
// the spinner renders to stderr.
func syncSection(label string, active bool, body func()) {
	if !active {
		fmt.Fprintf(os.Stderr, "  %s %s (no config)\n", output.Dim.Render("↷"), label)
		return
	}
	sp := output.NewSpinner(label)
	body()
	sp.Stop(label)
}
