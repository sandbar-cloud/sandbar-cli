package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type SitesCmd struct {
	List   SitesListCmd   `cmd:"" help:"List all sites."`
	Info   SitesInfoCmd   `cmd:"" help:"Show details for the current site."`
	Update SitesUpdateCmd `cmd:"" help:"Update the current site."`
	Delete SitesDeleteCmd `cmd:"" help:"Delete the current site."`
}

type SitesListCmd struct{}

func (cmd *SitesListCmd) Run(globals *Globals) error {
	c := globals.Client()
	resp, err := c.SearchSites("")
	if err != nil {
		return err
	}
	sites := resp.Data
	if len(sites) == 0 {
		fmt.Println("No sites. Run `sandbar init` to create one.")
		return nil
	}
	headers := []string{"SLUG", "LIVE URL", "LAST DEPLOY"}
	rows := make([][]string, len(sites))
	for i, s := range sites {
		rows[i] = []string{
			s.Slug,
			client.LiveURL(s.Slug),
			output.FormatTimeAgo(s.UpdatedAt),
		}
	}
	output.Table(headers, rows)
	return nil
}

type SitesInfoCmd struct{}

func (cmd *SitesInfoCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	site, err := c.GetSite(slug)
	if err != nil {
		return err
	}

	domains, _ := c.ListDomains(slug)
	deploys, _ := c.SearchDeploys(slug)

	fmt.Printf("  Name:           %s\n", output.Bold.Render(site.Name))
	fmt.Printf("  Slug:           %s\n", site.Slug)
	fmt.Printf("  Live URL:       %s\n", client.LiveURL(site.Slug))
	if site.ActiveDeployID != "" {
		activeDeploy, err := c.GetDeploy(slug, site.ActiveDeployID)
		if err == nil && activeDeploy.ActivatedAt != nil {
			fmt.Printf("  Active Deploy:  %s (%s)\n", site.ActiveDeployID, output.FormatTimeAgo(*activeDeploy.ActivatedAt))
		} else {
			fmt.Printf("  Active Deploy:  %s\n", site.ActiveDeployID)
		}
	}
	if domains != nil && len(domains.Items) > 0 {
		for _, d := range domains.Items {
			fmt.Printf("  Domain:         %s (%s, SSL %s)\n", d.Hostname, d.VerificationStatus, d.CertificateStatus)
		}
	}
	if deploys != nil && len(deploys.Data) > 0 {
		count := fmt.Sprintf("%d", len(deploys.Data))
		if deploys.HasMore {
			count += "+"
		}
		fmt.Printf("  Deploys:        %s\n", count)
	}
	return nil
}

type SitesUpdateCmd struct {
	Name             string `help:"New display name." short:"n"`
	ProductionBranch string `help:"Production branch name."`
}

func (cmd *SitesUpdateCmd) Run(globals *Globals) error {
	if cmd.Name == "" && cmd.ProductionBranch == "" {
		return fmt.Errorf("nothing to update: pass --name and/or --production-branch")
	}
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}

	req := client.UpdateSiteRequest{}
	if cmd.Name != "" {
		req.Name = &cmd.Name
	}
	if cmd.ProductionBranch != "" {
		req.ProductionBranch = &cmd.ProductionBranch
	}

	site, err := globals.Client().UpdateSite(slug, req)
	if err != nil {
		return err
	}
	fmt.Printf("Updated %s\n", output.Bold.Render(site.Slug))
	fmt.Printf("  Name: %s\n", site.Name)
	return nil
}

type SitesDeleteCmd struct {
	Yes bool `help:"Skip confirmation." short:"y"`
}

func (cmd *SitesDeleteCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	site, err := c.GetSite(slug)
	if err != nil {
		return err
	}

	if !cmd.Yes {
		fmt.Printf("This will permanently delete %s (%s)\n",
			output.Bold.Render(site.Name), client.LiveURL(site.Slug))
		fmt.Printf("Type the slug %q to confirm: ", site.Slug)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(answer) != site.Slug {
			fmt.Println("Aborted.")
			return nil
		}
	}

	sp := output.NewSpinner("Deleting...")
	if err := c.DeleteSite(slug); err != nil {
		sp.Fail("Delete failed")
		return err
	}
	sp.Stop(fmt.Sprintf("Deleted %s", site.Slug))
	return nil
}
