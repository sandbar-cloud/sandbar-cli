package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Ensure site exists. Probe with GET first — sites:read is in every
	// permission set (including the OIDC trust used by sandbar-action).
	// Only fall through to POST /sites if the site doesn't exist, which
	// is the local-dev "first deploy" case where the user has a Clerk
	// JWT with sites:write.
	sp := output.NewSpinner("Connecting to site...")
	if _, err := c.GetSite(slug); err != nil {
		apiErr, ok := err.(*client.APIError)
		if !ok || apiErr.StatusCode != 404 {
			sp.Fail("Failed to look up site")
			return err
		}
		// 404 — try to create.
		if _, createErr := c.CreateSite(client.CreateSiteRequest{Name: slug, Slug: slug}); createErr != nil {
			cErr, cOK := createErr.(*client.APIError)
			if cOK && cErr.StatusCode == 403 {
				sp.Fail("Site does not exist and CI lacks permission to create it")
				return fmt.Errorf(
					"site %q doesn't exist yet. Run `sandbar deploy` once locally to create it, "+
						"then re-run this CI deploy.", slug)
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

	// Reconcile custom domains against [[domains]] in .sandbar/config.toml.
	// Authoritative: server is brought into sync with config. Skipped when
	// the block is absent (nil) so projects that haven't adopted the
	// declarative shape aren't surprised by deletes.
	reconcileSite(c, slug, cfg.Site)
	if cfg.Domains != nil {
		reconcileDomains(c, slug, cfg.Domains)
	}
	if cfg.Preview.DefaultExpiry != "" {
		reconcilePreviewExpiry(c, slug, cfg.Preview.DefaultExpiry)
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
func reconcileDomains(c *client.Client, slug string, desired []config.DomainConfig) {
	resp, err := c.ListDomains(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! domain reconcile: list failed: %v\n", err)
		return
	}

	want := map[string]config.DomainConfig{}
	for _, d := range desired {
		want[d.Hostname] = d
	}
	have := map[string]client.Domain{}
	for _, d := range resp.Items {
		have[d.Hostname] = d
	}

	for host, d := range want {
		if _, ok := have[host]; ok {
			continue
		}
		if _, err := c.AddDomain(slug, client.AddDomainRequest{Hostname: host, RedirectTo: d.RedirectTo}); err != nil {
			fmt.Fprintf(os.Stderr, "  ! domain reconcile: add %s failed: %v\n", host, err)
			continue
		}
		fmt.Printf("  + domain added: %s (run `sandbar domains verify %s` after DNS is set)\n", host, host)
	}

	for host, d := range have {
		if _, ok := want[host]; ok {
			continue
		}
		if err := c.DeleteDomain(slug, d.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  ! domain reconcile: delete %s failed: %v\n", host, err)
			continue
		}
		fmt.Printf("  - domain removed: %s\n", host)
	}

	for host, d := range want {
		a, ok := have[host]
		if !ok || d.RedirectTo == a.RedirectTo {
			continue
		}
		newRedirect := d.RedirectTo
		if _, err := c.UpdateDomain(slug, a.ID, client.UpdateDomainRequest{RedirectTo: &newRedirect}); err != nil {
			fmt.Fprintf(os.Stderr, "  ! domain reconcile: update %s redirect_to failed: %v\n", host, err)
			continue
		}
		switch {
		case newRedirect == "":
			fmt.Printf("  ~ domain %s: cleared redirect (was %s)\n", host, a.RedirectTo)
		case a.RedirectTo == "":
			fmt.Printf("  ~ domain %s: now redirects to %s\n", host, newRedirect)
		default:
			fmt.Printf("  ~ domain %s: redirect changed %s → %s\n", host, a.RedirectTo, newRedirect)
		}
	}
}

// reconcileSite syncs the mutable [site] fields — display name and
// production_branch — against the server. A no-op when nothing in
// config sets a value (legacy configs that only have the slug under
// `site.name` skip the name sync entirely; see SiteConfig.DisplayName).
// Failures are warnings, not fatal — the deploy already succeeded.
func reconcileSite(c *client.Client, slug string, cfgSite config.SiteConfig) {
	desiredName := cfgSite.DisplayName()
	desiredBranch := cfgSite.ProductionBranch
	if desiredName == "" && desiredBranch == "" {
		return
	}

	site, err := c.GetSite(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! site reconcile: get site failed: %v\n", err)
		return
	}

	var req client.UpdateSiteRequest
	if desiredName != "" && site.Name != desiredName {
		req.Name = &desiredName
	}
	if desiredBranch != "" && site.ProductionBranch != desiredBranch {
		req.ProductionBranch = &desiredBranch
	}
	if req.Name == nil && req.ProductionBranch == nil {
		return
	}

	updated, err := c.UpdateSite(slug, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! site reconcile: update failed: %v\n", err)
		return
	}
	if req.Name != nil {
		fmt.Printf("  ~ site name: %q → %q\n", site.Name, updated.Name)
	}
	if req.ProductionBranch != nil {
		fmt.Printf("  ~ production branch: %s → %s\n", site.ProductionBranch, updated.ProductionBranch)
	}
}

// reconcilePreviewExpiry syncs the site's preview_expiry override
// against [preview] default_expiry from .sandbar/config.toml. A no-op
// when the server already matches; otherwise PATCHes the site. The
// server is authoritative on parse/validation — we just send the
// raw config string.
func reconcilePreviewExpiry(c *client.Client, slug, desired string) {
	site, err := c.GetSite(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! preview_expiry reconcile: get site failed: %v\n", err)
		return
	}
	if site.PreviewExpiry == desired {
		return
	}
	if _, err := c.UpdateSite(slug, client.UpdateSiteRequest{PreviewExpiry: &desired}); err != nil {
		fmt.Fprintf(os.Stderr, "  ! preview_expiry reconcile: update failed: %v\n", err)
		return
	}
	switch {
	case site.PreviewExpiry == "":
		fmt.Printf("  ~ preview expiry: %s (was platform default)\n", desired)
	default:
		fmt.Printf("  ~ preview expiry: %s → %s\n", site.PreviewExpiry, desired)
	}
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
