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

	slug := cfg.Site.Name
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

// runBuild executes cfg.Build.Command in workDir, streaming output to
// the terminal. Skips silently when there's no command configured or
// --skip-build is set.
func (cmd *DeployCmd) runBuild(cfg *config.ProjectConfig, workDir string) error {
	if cfg.Build.Command == "" {
		return nil
	}
	if cmd.SkipBuild {
		fmt.Println(output.Dim.Render("  Skipping build (--skip-build)"))
		return nil
	}

	fmt.Printf("  %s %s\n", output.Dim.Render("$"), cfg.Build.Command)
	c := exec.Command("sh", "-c", cfg.Build.Command)
	c.Dir = workDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
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
