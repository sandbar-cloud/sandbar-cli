package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type RollbackCmd struct {
	Yes bool `help:"Skip confirmation." short:"y"`
}

func (cmd *RollbackCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	site, err := c.GetSite(slug)
	if err != nil {
		return err
	}

	deploys, err := c.SearchDeploys(slug)
	if err != nil {
		return err
	}

	var prev string
	for _, d := range deploys.Data {
		if d.Status == "superseded" {
			prev = d.ID
			break
		}
	}
	if prev == "" {
		return fmt.Errorf("no previous deploy to roll back to")
	}

	if !cmd.Yes {
		fmt.Printf("Roll back from %s to %s? [y/N] ", site.ActiveDeployID, prev)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(answer)), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	sp := output.NewSpinner("Rolling back...")
	err = c.ActivateDeploy(slug, prev)
	if err != nil {
		sp.Fail("Rollback failed")
		return err
	}
	sp.Stop(fmt.Sprintf("Rolled back to %s", prev))
	fmt.Printf("  URL: %s\n", client.LiveURL(site.Slug))
	return nil
}
