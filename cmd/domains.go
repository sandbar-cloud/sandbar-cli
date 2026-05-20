package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type DomainsCmd struct {
	Add    DomainsAddCmd    `cmd:"" help:"Add a custom domain."`
	Update DomainsUpdateCmd `cmd:"" help:"Update settings on an existing domain."`
	List   DomainsListCmd   `cmd:"" help:"List domains."`
	Verify DomainsVerifyCmd `cmd:"" help:"Re-check domain verification."`
	Delete DomainsDeleteCmd `cmd:"" help:"Delete a custom domain."`
}

type DomainsAddCmd struct {
	Hostname   string `arg:"" help:"Domain hostname to add."`
	RedirectTo string `help:"Canonical hostname to 301 to. When set, this domain serves a redirect instead of content (e.g. www.example.com --redirect-to example.com)."`
}

func (cmd *DomainsAddCmd) Run(globals *Globals) error {
	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	slug := cfg.Site.Name
	if slug == "" {
		return fmt.Errorf("no site name in .sandbar/config.toml. Run `sandbar init`")
	}
	c := globals.Client()

	resp, err := c.AddDomain(slug, client.AddDomainRequest{
		Hostname:   cmd.Hostname,
		RedirectTo: cmd.RedirectTo,
	})
	if err != nil {
		return err
	}

	// Mirror the new domain into .sandbar/config.toml so subsequent
	// `sandbar deploy` reconcile passes see it as desired state.
	// Upsert by hostname — re-running `domains add` with a new
	// --redirect-to updates the entry in place.
	upserted := false
	for i, d := range cfg.Domains {
		if d.Hostname == cmd.Hostname {
			cfg.Domains[i].RedirectTo = cmd.RedirectTo
			upserted = true
			break
		}
	}
	if !upserted {
		cfg.Domains = append(cfg.Domains, config.DomainConfig{
			Hostname:   cmd.Hostname,
			RedirectTo: cmd.RedirectTo,
		})
	}
	if err := config.WriteProject(globals.WorkDir(), cfg); err != nil {
		// Don't fail the command — the API already accepted the
		// domain. Surface the warning so the user can hand-edit.
		fmt.Fprintf(os.Stderr, "warning: domain added on server but failed to update .sandbar/config.toml: %v\n", err)
	}

	fmt.Printf("\nAdd this DNS record to verify ownership of %s:\n\n", output.Bold.Render(cmd.Hostname))
	fmt.Printf("  Type:  %s\n", resp.DNSInstructions.RecordType)
	fmt.Printf("  Name:  %s\n", resp.DNSInstructions.RecordName)
	fmt.Printf("  Value: %s\n", resp.DNSInstructions.RecordValue)
	if cmd.RedirectTo != "" {
		fmt.Printf("\nOnce verified, %s will 301 to %s.\n",
			output.Bold.Render(cmd.Hostname),
			output.Bold.Render(cmd.RedirectTo),
		)
	}
	fmt.Printf("\nThen run: %s\n\n", output.Dim.Render("sandbar domains verify "+cmd.Hostname))
	return nil
}

type DomainsUpdateCmd struct {
	Hostname   string `arg:"" help:"Domain hostname to update."`
	RedirectTo string `help:"Set the canonical hostname this domain 301s to."`
	NoRedirect bool   `name:"no-redirect" help:"Clear the canonical redirect target so this domain serves content again."`
}

func (cmd *DomainsUpdateCmd) Run(globals *Globals) error {
	if cmd.RedirectTo != "" && cmd.NoRedirect {
		return fmt.Errorf("--redirect-to and --no-redirect are mutually exclusive")
	}
	if cmd.RedirectTo == "" && !cmd.NoRedirect {
		return fmt.Errorf("pass --redirect-to=<hostname> or --no-redirect")
	}

	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	slug := cfg.Site.Name
	if slug == "" {
		return fmt.Errorf("no site name in .sandbar/config.toml. Run `sandbar init`")
	}
	c := globals.Client()

	// Resolve the domain ID by hostname — the API is keyed by ID for
	// updates, but users think in hostnames.
	resp, err := c.ListDomains(slug)
	if err != nil {
		return err
	}
	var target *client.Domain
	for i, d := range resp.Items {
		if d.Hostname == cmd.Hostname {
			target = &resp.Items[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("domain %q not found on this site", cmd.Hostname)
	}

	newRedirect := cmd.RedirectTo // empty when --no-redirect was passed
	if _, err := c.UpdateDomain(slug, target.ID, client.UpdateDomainRequest{
		RedirectTo: &newRedirect,
	}); err != nil {
		return err
	}

	// Mirror the change into config.toml so the deploy reconcile
	// won't flag drift on the next run.
	upserted := false
	for i, d := range cfg.Domains {
		if d.Hostname == cmd.Hostname {
			cfg.Domains[i].RedirectTo = newRedirect
			upserted = true
			break
		}
	}
	if !upserted {
		cfg.Domains = append(cfg.Domains, config.DomainConfig{
			Hostname:   cmd.Hostname,
			RedirectTo: newRedirect,
		})
	}
	if err := config.WriteProject(globals.WorkDir(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: server updated but failed to update .sandbar/config.toml: %v\n", err)
	}

	if newRedirect == "" {
		fmt.Printf("Cleared redirect on %s. It will serve content directly.\n", output.Bold.Render(cmd.Hostname))
	} else {
		fmt.Printf("%s now redirects to %s.\n", output.Bold.Render(cmd.Hostname), output.Bold.Render(newRedirect))
	}
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

	domains := resp.Items
	if len(domains) == 0 {
		fmt.Println("No domains configured. Run `sandbar domains add <hostname>`.")
		return nil
	}

	headers := []string{"HOSTNAME", "VERIFICATION", "SSL", "REDIRECTS TO", "ADDED"}
	rows := make([][]string, len(domains))
	for i, d := range domains {
		redirectCell := "—"
		if d.RedirectTo != "" {
			redirectCell = d.RedirectTo
		}
		rows[i] = []string{
			d.Hostname,
			formatVerification(d.VerificationStatus),
			formatSSL(d.CertificateStatus),
			redirectCell,
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

	for _, d := range resp.Items {
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

type DomainsDeleteCmd struct {
	Hostname string `arg:"" help:"Domain hostname to remove."`
	Yes      bool   `help:"Skip confirmation." short:"y"`
}

func (cmd *DomainsDeleteCmd) Run(globals *Globals) error {
	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	slug := cfg.Site.Name
	if slug == "" {
		return fmt.Errorf("no site name in .sandbar/config.toml. Run `sandbar init`")
	}
	c := globals.Client()

	resp, err := c.ListDomains(slug)
	if err != nil {
		return err
	}

	var target *client.Domain
	for i, d := range resp.Items {
		if d.Hostname == cmd.Hostname {
			target = &resp.Items[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("domain %q not found on this site", cmd.Hostname)
	}

	if !cmd.Yes {
		fmt.Printf("Delete %s from %s? [y/N] ", output.Bold.Render(cmd.Hostname), slug)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(answer)), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	sp := output.NewSpinner("Deleting...")
	if err := c.DeleteDomain(slug, target.ID); err != nil {
		sp.Fail("Delete failed")
		return err
	}
	sp.Stop(fmt.Sprintf("Deleted %s", cmd.Hostname))

	// Remove from .sandbar/config.toml so the next reconcile doesn't
	// recreate it. Same warn-don't-fail pattern as `domains add`.
	kept := cfg.Domains[:0]
	for _, d := range cfg.Domains {
		if d.Hostname != cmd.Hostname {
			kept = append(kept, d)
		}
	}
	if len(kept) != len(cfg.Domains) {
		cfg.Domains = kept
		if err := config.WriteProject(globals.WorkDir(), cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: domain deleted on server but failed to update .sandbar/config.toml: %v\n", err)
		}
	}
	return nil
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
