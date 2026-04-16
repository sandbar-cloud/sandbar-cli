package cmd

import (
	"fmt"
	"time"

	"github.com/mataki-dev/sandbar-cli/internal/client"
	"github.com/mataki-dev/sandbar-cli/internal/output"
)

type DomainsCmd struct {
	Add    DomainsAddCmd    `cmd:"" help:"Add a custom domain."`
	List   DomainsListCmd   `cmd:"" help:"List domains."`
	Verify DomainsVerifyCmd `cmd:"" help:"Re-check domain verification."`
}

type DomainsAddCmd struct {
	Hostname string `arg:"" help:"Domain hostname to add."`
}

func (cmd *DomainsAddCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	resp, err := c.AddDomain(slug, client.AddDomainRequest{Hostname: cmd.Hostname})
	if err != nil {
		return err
	}

	fmt.Printf("\nAdd this DNS record to verify ownership of %s:\n\n", output.Bold.Render(cmd.Hostname))
	fmt.Printf("  Type:  %s\n", resp.DNSInstructions.RecordType)
	fmt.Printf("  Name:  %s\n", resp.DNSInstructions.RecordName)
	fmt.Printf("  Value: %s\n", resp.DNSInstructions.RecordValue)
	fmt.Printf("\nThen run: %s\n\n", output.Dim.Render("sandbar domains verify "+cmd.Hostname))
	return nil
}

type DomainsListCmd struct{}

func (cmd *DomainsListCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	resp, err := c.ListDomains(slug)
	if err != nil {
		return err
	}

	domains := resp.Data
	if len(domains) == 0 {
		fmt.Println("No domains configured. Run `sandbar domains add <hostname>`.")
		return nil
	}

	headers := []string{"HOSTNAME", "VERIFICATION", "SSL", "ADDED"}
	rows := make([][]string, len(domains))
	for i, d := range domains {
		rows[i] = []string{
			d.Hostname,
			formatVerification(d.VerificationStatus),
			formatSSL(d.CertificateStatus),
			d.CreatedAt.Format(time.DateOnly),
		}
	}
	output.Table(headers, rows)
	return nil
}

type DomainsVerifyCmd struct {
	Hostname string `arg:"" help:"Domain hostname to verify."`
}

func (cmd *DomainsVerifyCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	sp := output.NewSpinner(fmt.Sprintf("Checking %s...", cmd.Hostname))
	resp, err := c.ListDomains(slug)
	if err != nil {
		sp.Fail("Verification check failed")
		return err
	}

	for _, d := range resp.Data {
		if d.Hostname == cmd.Hostname {
			sp.Stop(fmt.Sprintf("%s  verification: %s  ssl: %s",
				output.Bold.Render(d.Hostname),
				formatVerification(d.VerificationStatus),
				formatSSL(d.CertificateStatus),
			))
			return nil
		}
	}
	sp.Fail("Domain not found")
	return fmt.Errorf("domain %q not found on this site", cmd.Hostname)
}

func formatVerification(status string) string {
	switch status {
	case "verified":
		return output.Green.Render("✓ verified")
	case "pending":
		return output.Yellow.Render("⏳ pending")
	case "failed":
		return output.Red.Render("✗ failed")
	default:
		return status
	}
}

func formatSSL(status string) string {
	switch status {
	case "active":
		return output.Green.Render("✓ active")
	case "provisioning":
		return output.Yellow.Render("provisioning")
	case "pending":
		return "— pending"
	case "failed":
		return output.Red.Render("✗ failed")
	default:
		return status
	}
}
