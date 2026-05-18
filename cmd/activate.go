package cmd

import (
	"fmt"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type ActivateCmd struct {
	DeployID string `arg:"" help:"Deploy ID to activate."`
}

func (cmd *ActivateCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	sp := output.NewSpinner("Activating...")
	err = c.ActivateDeploy(slug, cmd.DeployID)
	if err != nil {
		sp.Fail("Activation failed")
		return err
	}
	sp.Stop(fmt.Sprintf("Activated %s", cmd.DeployID))

	site, err := c.GetSite(slug)
	if err == nil {
		fmt.Printf("  URL: %s\n", client.LiveURL(site.Slug))
	}
	return nil
}
