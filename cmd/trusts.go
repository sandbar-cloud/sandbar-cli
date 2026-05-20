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

type TrustsCmd struct {
	Add    TrustsAddCmd    `cmd:"" help:"Add an OIDC deploy trust."`
	List   TrustsListCmd   `cmd:"" help:"List OIDC deploy trusts."`
	Delete TrustsDeleteCmd `cmd:"" help:"Remove an OIDC deploy trust."`
}

// ----- add -----

type TrustsAddCmd struct {
	Repository  string `arg:"" help:"Repository in '<owner>/<repo>' form (e.g. mataki-dev/mataki-web)."`
	Provider    string `default:"github" help:"OIDC provider."`
	RefFilter   string `name:"ref-filter" default:"*" help:"Git ref pattern this trust accepts (e.g. refs/heads/main)."`
	Environment string `default:"*" help:"GitHub Actions environment filter."`
}

func (cmd *TrustsAddCmd) Run(globals *Globals) error {
	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	slug := cfg.Site.EffectiveSlug()
	if slug == "" {
		return fmt.Errorf("no site slug in .sandbar/config.toml. Run `sandbar init`")
	}
	if !strings.Contains(cmd.Repository, "/") {
		return fmt.Errorf("repository must be in '<owner>/<repo>' form, got %q", cmd.Repository)
	}

	c := globals.Client()
	trust, err := c.AddTrust(slug, client.AddTrustRequest{
		Provider:    cmd.Provider,
		Repository:  cmd.Repository,
		RefFilter:   cmd.RefFilter,
		Environment: cmd.Environment,
	})
	if err != nil {
		return err
	}

	// Mirror the trust into config so the next deploy reconcile
	// won't flag drift. Upsert by tuple — re-running with the same
	// (repo, ref, env) is a no-op in config and would be a server-
	// side conflict if AddTrust were called again.
	entry := config.TrustConfig{
		Provider:    cmd.Provider,
		Repository:  cmd.Repository,
		RefFilter:   cmd.RefFilter,
		Environment: cmd.Environment,
	}
	key := entry.Key()
	upserted := false
	for _, t := range cfg.Trusts {
		if t.Key() == key {
			upserted = true
			break
		}
	}
	if !upserted {
		cfg.Trusts = append(cfg.Trusts, entry)
		if err := config.WriteProject(globals.WorkDir(), cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: trust added on server but failed to update .sandbar/config.toml: %v\n", err)
		}
	}

	fmt.Printf("Added trust %s:\n", output.Bold.Render(trust.ID))
	fmt.Printf("  repository:  %s\n", trust.Repository)
	fmt.Printf("  ref filter:  %s\n", trust.RefFilter)
	fmt.Printf("  environment: %s\n", trust.Environment)
	return nil
}

// ----- list -----

type TrustsListCmd struct{}

func (cmd *TrustsListCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	trusts, err := c.ListTrusts(slug)
	if err != nil {
		return err
	}
	if len(trusts) == 0 {
		fmt.Println("No OIDC trusts configured. Run `sandbar trusts add <repository>`.")
		return nil
	}

	headers := []string{"ID", "REPOSITORY", "REF FILTER", "ENVIRONMENT", "ADDED"}
	rows := make([][]string, len(trusts))
	for i, t := range trusts {
		rows[i] = []string{
			t.ID,
			t.Repository,
			t.RefFilter,
			t.Environment,
			t.CreatedAt.Format(time.DateOnly),
		}
	}
	output.Table(headers, rows)
	return nil
}

// ----- delete -----

type TrustsDeleteCmd struct {
	IDOrRepo string `arg:"" name:"id-or-repository" help:"Trust ID, or 'owner/repo' to match by repository when there's a single trust for it."`
	Yes      bool   `short:"y" help:"Skip confirmation."`
}

func (cmd *TrustsDeleteCmd) Run(globals *Globals) error {
	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	slug := cfg.Site.EffectiveSlug()
	if slug == "" {
		return fmt.Errorf("no site slug in .sandbar/config.toml. Run `sandbar init`")
	}
	c := globals.Client()

	trusts, err := c.ListTrusts(slug)
	if err != nil {
		return err
	}

	// Resolve the target: exact ID match wins; otherwise treat the
	// argument as a repository name and require a unique row.
	var target *client.Trust
	for i, t := range trusts {
		if t.ID == cmd.IDOrRepo {
			target = &trusts[i]
			break
		}
	}
	if target == nil {
		var matches []*client.Trust
		for i, t := range trusts {
			if t.Repository == cmd.IDOrRepo {
				matches = append(matches, &trusts[i])
			}
		}
		switch len(matches) {
		case 0:
			return fmt.Errorf("no trust matching %q on this site", cmd.IDOrRepo)
		case 1:
			target = matches[0]
		default:
			return fmt.Errorf("%d trusts match %q — pass the trust ID instead", len(matches), cmd.IDOrRepo)
		}
	}

	if !cmd.Yes {
		fmt.Printf("Delete trust %s (%s, ref=%s, env=%s) from %s? [y/N] ",
			output.Bold.Render(target.ID), target.Repository, target.RefFilter, target.Environment, slug)
		ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(ans)), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := c.DeleteTrust(slug, target.ID); err != nil {
		return err
	}

	// Mirror the delete into config.toml. Match by tuple — same
	// approach the reconcile uses.
	key := config.TrustKey{
		Provider:    target.Provider,
		Repository:  target.Repository,
		RefFilter:   target.RefFilter,
		Environment: target.Environment,
	}
	kept := cfg.Trusts[:0]
	for _, t := range cfg.Trusts {
		if t.Key() != key {
			kept = append(kept, t)
		}
	}
	if len(kept) != len(cfg.Trusts) {
		cfg.Trusts = kept
		if err := config.WriteProject(globals.WorkDir(), cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: trust deleted on server but failed to update .sandbar/config.toml: %v\n", err)
		}
	}

	fmt.Printf("Deleted trust %s\n", target.ID)
	return nil
}
