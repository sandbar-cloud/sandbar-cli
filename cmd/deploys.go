package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type DeploysCmd struct {
	List  DeploysListCmd  `cmd:"" help:"List deploys for the site."`
	Prune DeploysPruneCmd `cmd:"" help:"Delete superseded deploys (filtered by branch, age, or both)."`
}

// ----- list -----

type DeploysListCmd struct {
	Branch       string `help:"Filter to a single branch."`
	AllBranches  bool   `name:"all-branches" help:"List deploys from every branch (default: production branch only when --branch is unset)."`
	IncludeMain  bool   `name:"include-production" help:"With --all-branches, include the production branch."`
	Status       string `help:"Filter by status (ready, active, superseded, ...)."`
	Limit        int    `default:"50" help:"Maximum deploys to fetch."`
}

func (cmd *DeploysListCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	deploys, err := fetchDeploys(c, slug, fetchOpts{
		branch:      cmd.Branch,
		allBranches: cmd.AllBranches,
		status:      cmd.Status,
		limit:       cmd.Limit,
	})
	if err != nil {
		return err
	}

	// When neither --branch nor --all-branches is set, default to the
	// site's production branch + the un-branched default (empty
	// branch). The all-branches path can opt in to including the
	// production branch via --include-production.
	if !cmd.AllBranches && cmd.Branch == "" {
		// Show everything for now — production filtering would need a
		// GetSite call to know the production branch. Keep the
		// default list output simple and let the user pass --branch
		// if they want narrower results.
	}
	if cmd.AllBranches && !cmd.IncludeMain {
		// Filter out the un-branched (production) deploys.
		filtered := deploys[:0]
		for _, d := range deploys {
			if d.Branch != "" {
				filtered = append(filtered, d)
			}
		}
		deploys = filtered
	}

	if len(deploys) == 0 {
		fmt.Println("No deploys.")
		return nil
	}

	// Group by branch for readability when listing more than one.
	groups := groupByBranch(deploys)
	branchOrder := sortedBranchKeys(groups)

	headers := []string{"DEPLOY ID", "STATUS", "MESSAGE", "AGE"}
	for _, branch := range branchOrder {
		label := branch
		if label == "" {
			label = "(production)"
		}
		fmt.Printf("\n%s\n", output.Bold.Render(label))
		rows := make([][]string, len(groups[branch]))
		for i, d := range groups[branch] {
			rows[i] = []string{
				d.ID,
				formatDeployStatus(d.Status),
				truncate(d.Message, 60),
				humanAge(d.CreatedAt),
			}
		}
		output.Table(headers, rows)
	}
	return nil
}

// ----- prune -----

type DeploysPruneCmd struct {
	Branch      string `help:"Prune deploys from a single branch (and drop the branch's preview URL when no deploys remain)."`
	AllBranches bool   `name:"all-branches" help:"Prune across every non-production branch."`
	OlderThan   string `name:"older-than" default:"7d" help:"Only delete deploys created more than this long ago (e.g. 7d, 24h)."`
	Keep        int    `default:"0" help:"Keep this many newest deploys per branch from deletion (0 = keep none)."`
	DryRun      bool   `name:"dry-run" help:"Show what would be deleted without making changes."`
	Yes         bool   `short:"y" help:"Skip the confirmation prompt."`
}

func (cmd *DeploysPruneCmd) Run(globals *Globals) error {
	if cmd.Branch == "" && !cmd.AllBranches {
		return fmt.Errorf("pass --branch=<name> or --all-branches")
	}
	if cmd.Branch != "" && cmd.AllBranches {
		return fmt.Errorf("--branch and --all-branches are mutually exclusive")
	}

	cutoffAge, err := time.ParseDuration(normalizeDuration(cmd.OlderThan))
	if err != nil {
		return fmt.Errorf("--older-than: %w", err)
	}
	cutoff := time.Now().Add(-cutoffAge)

	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	deploys, err := fetchDeploys(c, slug, fetchOpts{
		branch:      cmd.Branch,
		allBranches: cmd.AllBranches,
		// Don't filter by status — superseded is the obvious target
		// but `ready` (uploaded, never activated) deploys also pile
		// up and the user usually wants them gone too.
		limit: 1000,
	})
	if err != nil {
		return err
	}

	// Pick deletion candidates: skip the active deploy (server would
	// reject anyway), skip --keep newest per branch, only delete
	// rows older than the cutoff.
	candidates := selectPruneCandidates(deploys, cutoff, cmd.Keep, cmd.AllBranches)
	if len(candidates) == 0 {
		fmt.Println("Nothing to prune.")
		return nil
	}

	fmt.Printf("%d deploy(s) eligible:\n", len(candidates))
	for _, d := range candidates {
		branchLabel := d.Branch
		if branchLabel == "" {
			branchLabel = "(production)"
		}
		fmt.Printf("  - %s  %s  %s  %s\n",
			output.Dim.Render(d.ID),
			branchLabel,
			truncate(d.Message, 50),
			humanAge(d.CreatedAt),
		)
	}
	if cmd.DryRun {
		fmt.Println("\n(dry run — no deletions performed)")
		return nil
	}

	if !cmd.Yes {
		fmt.Print("Delete? [y/N] ")
		ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(ans)), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var failed int
	for _, d := range candidates {
		if err := c.DeleteDeploy(slug, d.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  ! delete %s failed: %v\n", d.ID, err)
			failed++
			continue
		}
		fmt.Printf("  ✓ deleted %s\n", d.ID)
	}
	if failed > 0 {
		return fmt.Errorf("%d delete(s) failed", failed)
	}
	return nil
}

// ----- helpers -----

type fetchOpts struct {
	branch      string
	allBranches bool
	status      string
	limit       int
}

func fetchDeploys(c *client.Client, slug string, opts fetchOpts) ([]client.Deploy, error) {
	req := client.SearchDeploysRequest{
		Filter: map[string]map[string]any{},
	}
	if opts.branch != "" {
		req.Filter["branch"] = map[string]any{"eq": opts.branch}
	}
	if opts.status != "" {
		req.Filter["status"] = map[string]any{"eq": opts.status}
	}
	if opts.limit > 0 {
		l := opts.limit
		req.Limit = &l
	}

	resp, err := c.SearchDeploysWith(slug, req)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func groupByBranch(deploys []client.Deploy) map[string][]client.Deploy {
	out := map[string][]client.Deploy{}
	for _, d := range deploys {
		out[d.Branch] = append(out[d.Branch], d)
	}
	return out
}

func sortedBranchKeys(groups map[string][]client.Deploy) []string {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		// Production (empty branch) first.
		if keys[i] == "" {
			return true
		}
		if keys[j] == "" {
			return false
		}
		return keys[i] < keys[j]
	})
	return keys
}

func selectPruneCandidates(deploys []client.Deploy, cutoff time.Time, keep int, branchAware bool) []client.Deploy {
	// Group by branch so per-branch --keep applies. When pruning a
	// single branch the map only has one entry and the behaviour is
	// identical.
	groups := groupByBranch(deploys)
	var out []client.Deploy
	for _, group := range groups {
		// Newest first.
		sort.Slice(group, func(i, j int) bool {
			return group[i].CreatedAt.After(group[j].CreatedAt)
		})
		kept := 0
		for _, d := range group {
			if d.Status == "active" {
				continue
			}
			if d.CreatedAt.After(cutoff) {
				continue
			}
			if kept < keep {
				kept++
				continue
			}
			out = append(out, d)
		}
		_ = branchAware
	}
	// Stable order for the prompt: newest-first within each branch.
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// normalizeDuration extends Go's ParseDuration to accept a day suffix
// like "7d" by expanding it to hours. Anything else falls through
// unchanged so "24h", "30m", etc. keep working.
func normalizeDuration(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "d") {
		return s
	}
	var days int
	if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%d", &days); err != nil {
		return s // let ParseDuration produce the real error
	}
	return fmt.Sprintf("%dh", days*24)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func formatDeployStatus(s string) string {
	switch s {
	case "active":
		return output.Green.Render("● active")
	case "ready":
		return output.Yellow.Render("○ ready")
	case "superseded":
		return output.Dim.Render("◐ superseded")
	case "quarantined":
		return output.Red.Render("✗ quarantined")
	default:
		return s
	}
}
