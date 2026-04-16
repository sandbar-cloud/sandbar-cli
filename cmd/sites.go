package cmd

import (
	"fmt"

	"github.com/mataki-dev/sandbar-cli/internal/output"
)

type SitesCmd struct {
	List SitesListCmd `cmd:"" help:"List all sites."`
	Info SitesInfoCmd `cmd:"" help:"Show details for the current site."`
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
			fmt.Sprintf("https://%s.sandbar.cloud", s.Slug),
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
	fmt.Printf("  Live URL:       https://%s.sandbar.cloud\n", site.Slug)
	if site.ActiveDeployID != "" {
		activeDeploy, err := c.GetDeploy(slug, site.ActiveDeployID)
		if err == nil && activeDeploy.ActivatedAt != nil {
			fmt.Printf("  Active Deploy:  %s (%s)\n", site.ActiveDeployID, output.FormatTimeAgo(*activeDeploy.ActivatedAt))
		} else {
			fmt.Printf("  Active Deploy:  %s\n", site.ActiveDeployID)
		}
	}
	if domains != nil && len(domains.Data) > 0 {
		for _, d := range domains.Data {
			fmt.Printf("  Domain:         %s (%s, SSL %s)\n", d.Hostname, d.VerificationStatus, d.CertificateStatus)
		}
	}
	if deploys != nil {
		fmt.Printf("  Deploys:        %d total\n", deploys.TotalCount)
	}
	return nil
}
