package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
	"github.com/sandbar-cloud/sandbar-cli/internal/git"
	"github.com/sandbar-cloud/sandbar-cli/internal/hasher"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
	"github.com/sandbar-cloud/sandbar-cli/internal/uploader"
)

type DeployCmd struct {
	Dir         string `help:"Build output directory." short:"d"`
	NoActivate  bool   `help:"Upload without activating." name:"no-activate"`
	SkipBuild   bool   `help:"Skip the configured build command." name:"skip-build" env:"SANDBAR_SKIP_BUILD"`
	Env         string `help:"Named environment to apply (matches [env.<name>] in sandbar config)." short:"e" env:"SANDBAR_ENV"`
	Message     string `help:"Deploy message." short:"m"`
	Branch      string `help:"Branch name for branch deploys." short:"b"`
	Concurrency int    `help:"Parallel upload workers." default:"8" short:"c"`
}

func (cmd *DeployCmd) Run(globals *Globals) error {
	cfg, err := globals.ProjectConfig()
	if err != nil {
		return err
	}
	c := globals.Client()
	buildDir := cmd.resolveBuildDir(cfg)
	return cmd.RunWith(c, globals.WorkDir(), buildDir, cfg)
}

func (cmd *DeployCmd) RunWith(c *client.Client, workDir, buildDir string, cfg *config.ProjectConfig) error {
	start := time.Now()

	if cfg == nil {
		var err error
		cfg, err = config.LoadProject(workDir)
		if err != nil {
			return err
		}
	}

	slug := cfg.Site.EffectiveSlug()
	if slug == "" {
		return fmt.Errorf("no site name in .sandbar/config.toml. Run `sandbar init`")
	}

	// Ensure site exists. Probe with GET first — sites:read is broadly
	// granted. Only fall through to POST /sites if the site doesn't
	// exist; creating one needs sites:write, which not every token
	// carries (CI tokens never do, and an operator token only does if
	// their role grants it).
	sp := output.NewSpinner("Connecting to site...")
	if _, err := c.GetSite(slug); err != nil {
		apiErr, ok := err.(*client.APIError)
		if !ok || apiErr.StatusCode != 404 {
			sp.Fail("Failed to look up site")
			return err
		}
		// 404 — try to create. A 403 here means this token lacks
		// sites:write, not specifically that we're in CI.
		if _, createErr := c.CreateSite(client.CreateSiteRequest{Name: slug, Slug: slug}); createErr != nil {
			cErr, cOK := createErr.(*client.APIError)
			if cOK && cErr.StatusCode == 403 {
				sp.Fail("Cannot create site: token lacks sites:write")
				if runningInCI() {
					return fmt.Errorf(
						"site %q doesn't exist and this CI token can't create sites (needs sites:write). "+
							"Have an operator with sites:write create it once (`sandbar deploy` locally), then re-run CI.", slug)
				}
				return fmt.Errorf(
					"site %q doesn't exist and your token lacks the sites:write scope to create it. "+
						"Your operator role must grant sites:write — fix that, then `sandbar login` again.", slug)
			}
			if !cOK || cErr.StatusCode != 409 {
				sp.Fail("Failed to create site")
				return createErr
			}
		}
	}
	sp.Stop(fmt.Sprintf("Site: %s", slug))

	// If --env names a table, it must exist. Skipping this check would
	// silently fall back to defaults on a typo'd flag.
	if cmd.Env != "" && !cfg.HasEnv(cmd.Env) {
		return fmt.Errorf("environment %q not defined in .sandbar/config.toml ([env.%s])", cmd.Env, cmd.Env)
	}

	// Run configured build command. Streams stdout/stderr live so the
	// user sees their build tool's output as it runs.
	if err := cmd.runBuild(cfg, workDir); err != nil {
		return err
	}

	// Resolve message
	message := cmd.Message
	if message == "" && cfg.Deploy.MessageFromGit {
		message = git.HeadMessage(workDir)
	}

	// Resolve branch and commit SHA
	branch := cmd.Branch
	if branch == "" {
		branch = git.BranchName(workDir)
	}
	commitSHA := git.HeadSHA(workDir)

	// Hash files
	fullBuildDir := filepath.Join(workDir, buildDir)
	sp = output.NewSpinner("Analyzing files...")
	entries, err := hasher.HashDir(fullBuildDir, func(path string, completed, total int) {
		sp.UpdateMsg(fmt.Sprintf("Analyzing %s (%d/%d)", path, completed, total))
	})
	if err != nil {
		sp.Fail("Analysis failed")
		return fmt.Errorf("build directory '%s' not found. Run your build command first, or use --dir", buildDir)
	}
	if len(entries) == 0 {
		sp.Fail("No files found")
		return fmt.Errorf("build directory '%s' is empty", buildDir)
	}

	var totalBytes int64
	sizeMap := make(map[string]int64, len(entries))
	manifest := make([]client.FileEntry, len(entries))
	for i, e := range entries {
		manifest[i] = client.FileEntry{Path: e.Path, Hash: e.Hash, Size: e.Size}
		sizeMap[e.Path] = e.Size
		totalBytes += e.Size
	}
	sp.Stop(fmt.Sprintf("Analyzed %d files (%s)", len(entries), output.FormatBytes(totalBytes)))

	// Build redirect/header rules from config
	var redirects []client.RedirectRule
	for _, rule := range cfg.Redirects {
		redirects = append(redirects, client.RedirectRule{From: rule.From, To: rule.To, Status: rule.Status})
	}
	var headers []client.HeaderRule
	for _, rule := range cfg.Headers {
		headers = append(headers, client.HeaderRule{Pattern: rule.For, Headers: rule.Values})
	}

	// Create deploy. The API checks every file against existing blobs
	// and mints signed upload URLs — for large manifests this takes
	// a few seconds, so show a spinner.
	sp = output.NewSpinner("Preparing upload...")
	resp, err := c.CreateDeploy(slug, client.CreateDeployRequest{
		Message:      message,
		Branch:       branch,
		CommitSHA:    commitSHA,
		FileManifest: manifest,
		Redirects:    redirects,
		Headers:      headers,
	})
	if err != nil {
		sp.Fail("Failed to prepare upload")
		return err
	}
	if len(resp.Uploads) > 0 {
		sp.Stop(fmt.Sprintf("Prepared %d new files (%d reused)", len(resp.Uploads), resp.SkippedCount))
	} else {
		sp.Stop(fmt.Sprintf("All %d files already on Sandbar", resp.SkippedCount))
	}

	// Upload changed files
	if len(resp.Uploads) > 0 {
		var uploadBytes int64
		items := make([]uploader.UploadItem, len(resp.Uploads))
		for i, u := range resp.Uploads {
			size := sizeMap[u.Path]
			uploadBytes += size
			items[i] = uploader.UploadItem{
				LocalPath: filepath.Join(fullBuildDir, u.Path),
				SignedURL: u.UploadURL,
				FilePath:  u.Path,
				Size:      size,
			}
		}

		concurrency := cmd.Concurrency
		if concurrency < 1 {
			concurrency = 8
		}

		bp := output.NewBlobProgress(len(items), uploadBytes)
		err = uploader.Upload(items, concurrency, func(e uploader.BlobEvent) {
			switch e.Type {
			case uploader.BlobStarted:
				bp.BlobStarted(e.Index, e.FilePath, e.Size)
			case uploader.BlobProgress:
				bp.BlobUploaded(e.Index, e.Uploaded)
			case uploader.BlobDone:
				bp.BlobDone(e.Index)
			}
		})
		if err != nil {
			bp.Fail("Upload failed")
			return err
		}
		bp.Stop(fmt.Sprintf("Pushed %d files (%s)", len(items), output.FormatBytes(uploadBytes)))
	} else {
		fmt.Println("  No files changed, skipping upload")
	}

	// Finalize
	sp = output.NewSpinner("Finalizing...")
	if err = c.FinalizeDeploy(slug, resp.DeployID); err != nil {
		sp.Fail("Finalize failed")
		return err
	}

	// Poll for scanning to complete (max 60s)
	const maxScanPolls = 60
	deploy, err := c.GetDeploy(slug, resp.DeployID)
	if err != nil {
		sp.Fail("Status check failed")
		return err
	}
	for polls := 0; deploy.Status == "scanning"; polls++ {
		if polls >= maxScanPolls {
			sp.Fail("Scan timed out")
			return fmt.Errorf("deploy scan timed out. Check the console for status")
		}
		time.Sleep(1 * time.Second)
		deploy, err = c.GetDeploy(slug, resp.DeployID)
		if err != nil {
			sp.Fail("Status check failed")
			return err
		}
	}

	if deploy.Status == "quarantined" {
		sp.Fail("Deploy blocked by content safety scan. Contact support.")
		return fmt.Errorf("deploy quarantined")
	}
	sp.Stop("Scanning... clean")

	// Activate (unless --no-activate)
	if cmd.NoActivate {
		fmt.Printf("\n  Deploy: %s (staged, not activated)\n\n", output.Bold.Render(resp.DeployID))
		return nil
	}

	sp = output.NewSpinner("Activating...")
	if err = c.ActivateDeploy(slug, resp.DeployID); err != nil {
		sp.Fail("Activation failed")
		return err
	}
	sp.Stop(fmt.Sprintf("Deployed in %s", time.Since(start).Round(100*time.Millisecond)))

	// Reconcile site-level state — domains, trusts, name, production
	// branch, preview expiry — against the config. Only on production
	// deploys, never on previews: a PR-branch deploy could otherwise
	// push unmerged config to the server, or delete domains/trusts that
	// the PR branch hasn't picked up yet.
	if isProductionDeploy(branch, cfg.Site.ProductionBranch) {
		reports := []reconcileReport{reconcileSite(c, slug, cfg.Site)}
		if cfg.Domains != nil {
			reports = append(reports, reconcileDomains(c, slug, cfg.Domains))
		}
		if cfg.Trusts != nil {
			reports = append(reports, reconcileTrusts(c, slug, cfg.Trusts))
		}
		if cfg.Preview.DefaultExpiry != "" {
			reports = append(reports, reconcilePreviewExpiry(c, slug, cfg.Preview.DefaultExpiry))
		}
		// Quiet on a clean no-op; otherwise each note on its own line.
		for _, rep := range reports {
			for _, n := range rep.notes {
				fmt.Fprintf(os.Stderr, "  %s\n", n)
			}
		}
	}

	site, err := c.GetSite(slug)
	if err != nil {
		// Non-fatal: still print deploy ID even if site fetch fails
		fmt.Printf("  Deploy: %s\n", output.Bold.Render(resp.DeployID))
		return nil
	}
	fmt.Printf("  Deploy: %s\n", output.Bold.Render(resp.DeployID))
	fmt.Printf("  URL:    %s\n", output.Bold.Render(client.LiveURL(site.Slug)))
	if branch != "" && branch != "main" {
		fmt.Printf("  Preview: %s\n", output.Bold.Render(client.PreviewURL(site.Slug, branch)))
	}
	return nil
}

// reconcileDomains brings the server's custom_domains for a site into
// sync with the desired list from .sandbar/config.toml. Adds missing
// entries (kicks off the normal verification flow) and deletes
// orphans. Drift in `redirect_to` on an existing domain is warned
// about but not corrected — the server has no update-redirect-to
// endpoint yet, so the workaround is delete + re-add.
// reconcileReport is the outcome of one reconcile step: human-readable
// notes (each already marked — "! " warning, "+ " add, "- " remove,
// "~ " change) and whether the step ran without a server/permission
// error. Reconcilers return this instead of printing directly, so the
// caller renders after any live spinner has released the output stream
// (printing mid-spinner corrupts the spinner's cursor tracking).
type reconcileReport struct {
	notes []string
	ok    bool
}

func (r *reconcileReport) note(format string, a ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, a...))
}

func (r *reconcileReport) fail(format string, a ...any) {
	r.note(format, a...)
	r.ok = false
}

func reconcileDomains(c *client.Client, slug string, desired []config.DomainConfig) reconcileReport {
	r := reconcileReport{ok: true}
	resp, err := c.ListDomains(slug)
	if err != nil {
		r.fail("! domain reconcile: list failed: %v", err)
		return r
	}

	want := map[string]config.DomainConfig{}
	for _, d := range desired {
		host := strings.TrimSpace(d.Hostname)
		if host == "" {
			r.note("! domain reconcile: skipping [[domains]] entry with empty hostname")
			continue
		}
		if !strings.Contains(host, ".") {
			r.note("! domain reconcile: skipping invalid hostname %q (must contain a dot)", host)
			continue
		}
		want[host] = d
	}
	have := map[string]client.Domain{}
	for _, d := range resp.Items {
		have[d.Hostname] = d
	}

	addFailed := 0
	for host, d := range want {
		if _, ok := have[host]; ok {
			continue
		}
		if _, err := c.AddDomain(slug, client.AddDomainRequest{Hostname: host, RedirectTo: d.RedirectTo}); err != nil {
			r.fail("! domain reconcile: add %s failed: %v", host, err)
			addFailed++
			continue
		}
		r.note("+ domain added: %s (run `sandbar domains verify %s` after DNS is set)", host, host)
	}

	// Safety: same rationale as reconcileTrusts — if any add failed,
	// skip every delete so a failed replacement doesn't leave the site
	// without the domain a user expected to be there.
	if addFailed > 0 {
		r.fail("! domain reconcile: %d add(s) failed — skipping delete phase to preserve existing domains", addFailed)
		return r
	}

	for host, d := range have {
		if _, ok := want[host]; ok {
			continue
		}
		if err := c.DeleteDomain(slug, d.ID); err != nil {
			r.fail("! domain reconcile: delete %s failed: %v", host, err)
			continue
		}
		r.note("- domain removed: %s", host)
	}

	for host, d := range want {
		a, ok := have[host]
		if !ok || d.RedirectTo == a.RedirectTo {
			continue
		}
		newRedirect := d.RedirectTo
		if _, err := c.UpdateDomain(slug, a.ID, client.UpdateDomainRequest{RedirectTo: &newRedirect}); err != nil {
			r.fail("! domain reconcile: update %s redirect_to failed: %v", host, err)
			continue
		}
		switch {
		case newRedirect == "":
			r.note("~ domain %s: cleared redirect (was %s)", host, a.RedirectTo)
		case a.RedirectTo == "":
			r.note("~ domain %s: now redirects to %s", host, newRedirect)
		default:
			r.note("~ domain %s: redirect changed %s → %s", host, a.RedirectTo, newRedirect)
		}
	}
	return r
}

// isProductionDeploy decides whether site-level reconcile should run.
// True when:
//   - The deploy has no branch set (un-branched default), or
//   - The branch matches the configured production branch (or "main"
//     as the server-side default when config doesn't override it).
//
// Everything else is a preview and must not push config-driven state
// to the server.
func isProductionDeploy(branch, configuredProduction string) bool {
	if branch == "" {
		return true
	}
	prod := configuredProduction
	if prod == "" {
		prod = "main"
	}
	return branch == prod
}

// runningInCI reports whether we're in a CI runner. Used only to tailor
// the "can't create site" guidance — not for auth decisions.
func runningInCI() bool {
	return os.Getenv("GITHUB_ACTIONS") != "" || os.Getenv("CI") != ""
}

// reconcileTrusts brings the server's OIDC trust list into sync with
// [[trusts]] in .sandbar/config.toml. Authoritative: trusts present
// on the server but absent from config are deleted. Skipped at the
// call site when the block is nil (project hasn't adopted the
// declarative shape).
//
// Footgun: the trust authenticating this very deploy can be the one
// deleted. The reconcile already succeeded auth-wise, so the current
// request finishes — but the next workflow run breaks. The CLI
// surfaces the delete in its output so users see what changed.
func reconcileTrusts(c *client.Client, slug string, desired []config.TrustConfig) reconcileReport {
	r := reconcileReport{ok: true}
	remote, err := c.ListTrusts(slug)
	if err != nil {
		r.fail("! trust reconcile: list failed: %v", err)
		return r
	}

	want := map[config.TrustKey]config.TrustConfig{}
	for _, t := range desired {
		want[t.Key()] = t
	}
	have := map[config.TrustKey]client.Trust{}
	for _, t := range remote {
		have[config.TrustKey{
			Provider:    t.Provider,
			Repository:  t.Repository,
			RefFilter:   t.RefFilter,
			Environment: t.Environment,
		}] = t
	}

	addFailed := 0
	for key, t := range want {
		if _, ok := have[key]; ok {
			continue
		}
		if _, err := c.AddTrust(slug, client.AddTrustRequest{
			Repository:  t.Repository,
			RefFilter:   t.EffectiveRefFilter(),
			Environment: t.EffectiveEnvironment(),
		}); err != nil {
			r.fail("! trust reconcile: add %s (ref=%s env=%s) failed: %v",
				t.Repository, t.EffectiveRefFilter(), t.EffectiveEnvironment(), err)
			addFailed++
			continue
		}
		r.note("+ trust added: %s (ref=%s env=%s)",
			t.Repository, t.EffectiveRefFilter(), t.EffectiveEnvironment())
	}

	// Safety: if any add failed, skip every delete. The user may have
	// meant to replace an existing trust they're about to delete — a
	// failed add followed by a successful delete would orphan them out
	// of their own site (the auth trust would be gone).
	if addFailed > 0 {
		r.fail("! trust reconcile: %d add(s) failed — skipping delete phase to preserve existing trusts", addFailed)
		return r
	}

	for key, t := range have {
		if _, ok := want[key]; ok {
			continue
		}
		if err := c.DeleteTrust(slug, t.ID); err != nil {
			r.fail("! trust reconcile: delete %s failed: %v", t.ID, err)
			continue
		}
		r.note("- trust removed: %s (ref=%s env=%s)", t.Repository, t.RefFilter, t.Environment)
	}
	return r
}

// reconcileSite syncs the mutable [site] fields — display name and
// production_branch — against the server. A no-op when nothing in
// config sets a value (legacy configs that only have the slug under
// `site.name` skip the name sync entirely; see SiteConfig.DisplayName).
// Failures are warnings, not fatal — the deploy already succeeded.
func reconcileSite(c *client.Client, slug string, cfgSite config.SiteConfig) reconcileReport {
	r := reconcileReport{ok: true}
	desiredName := cfgSite.DisplayName()
	desiredBranch := cfgSite.ProductionBranch
	if desiredName == "" && desiredBranch == "" {
		return r
	}

	site, err := c.GetSite(slug)
	if err != nil {
		r.fail("! site reconcile: get site failed: %v", err)
		return r
	}

	var req client.UpdateSiteRequest
	if desiredName != "" && site.Name != desiredName {
		req.Name = &desiredName
	}
	if desiredBranch != "" && site.ProductionBranch != desiredBranch {
		req.ProductionBranch = &desiredBranch
	}
	if req.Name == nil && req.ProductionBranch == nil {
		return r
	}

	updated, err := c.UpdateSite(slug, req)
	if err != nil {
		r.fail("! site reconcile: update failed: %v", err)
		return r
	}
	if req.Name != nil {
		r.note("~ site name: %q → %q", site.Name, updated.Name)
	}
	if req.ProductionBranch != nil {
		r.note("~ production branch: %s → %s", site.ProductionBranch, updated.ProductionBranch)
	}
	return r
}

// reconcilePreviewExpiry syncs the site's preview_expiry override
// against [preview] default_expiry from .sandbar/config.toml. A no-op
// when the server already matches; otherwise PATCHes the site. The
// server is authoritative on parse/validation — we just send the
// raw config string.
func reconcilePreviewExpiry(c *client.Client, slug, desired string) reconcileReport {
	r := reconcileReport{ok: true}
	site, err := c.GetSite(slug)
	if err != nil {
		r.fail("! preview_expiry reconcile: get site failed: %v", err)
		return r
	}
	if site.PreviewExpiry == desired {
		return r
	}
	if _, err := c.UpdateSite(slug, client.UpdateSiteRequest{PreviewExpiry: &desired}); err != nil {
		r.fail("! preview_expiry reconcile: update failed: %v", err)
		return r
	}
	switch {
	case site.PreviewExpiry == "":
		r.note("~ preview expiry: %s (was platform default)", desired)
	default:
		r.note("~ preview expiry: %s → %s", site.PreviewExpiry, desired)
	}
	return r
}

// runBuild executes cfg.Build.Command in workDir, streaming output to
// the terminal. Skips silently when there's no command configured or
// --skip-build is set. Merges [env] defaults + [env.<cmd.Env>]
// overrides on top of the inherited environment; later entries win.
func (cmd *DeployCmd) runBuild(cfg *config.ProjectConfig, workDir string) error {
	if cfg.Build.Command == "" {
		return nil
	}
	if cmd.SkipBuild {
		fmt.Println(output.Dim.Render("  Skipping build (--skip-build)"))
		return nil
	}

	if cmd.Env != "" {
		fmt.Printf("  %s env=%s\n", output.Dim.Render("$"), cmd.Env)
	}
	fmt.Printf("  %s %s\n", output.Dim.Render("$"), cfg.Build.Command)
	c := exec.Command("sh", "-c", cfg.Build.Command)
	c.Dir = workDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = mergeEnv(os.Environ(), cfg.EnvFor(cmd.Env))
	if err := c.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}

// mergeEnv returns base with overrides applied (overrides win). Keys
// in overrides replace any same-name entry in base; new keys are
// appended. The result is in the os/exec "KEY=VALUE" form.
func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]bool, len(overrides))
	for _, kv := range base {
		eq := -1
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				eq = i
				break
			}
		}
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		k := kv[:eq]
		if v, ok := overrides[k]; ok {
			out = append(out, k+"="+v)
			seen[k] = true
		} else {
			out = append(out, kv)
		}
	}
	for k, v := range overrides {
		if !seen[k] {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func (cmd *DeployCmd) resolveBuildDir(cfg *config.ProjectConfig) string {
	if cmd.Dir != "" {
		return cmd.Dir
	}
	if cfg.Site.BuildDir != "" {
		return cfg.Site.BuildDir
	}
	return "dist"
}
