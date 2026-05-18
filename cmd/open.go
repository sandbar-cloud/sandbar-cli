package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/mataki-dev/sandbar-cli/internal/output"
)

type OpenCmd struct {
	Console bool `help:"Open console for this site." short:"c"`
}

func (cmd *OpenCmd) Run(globals *Globals) error {
	slug, err := globals.SiteSlug()
	if err != nil {
		return err
	}
	c := globals.Client()

	site, err := c.GetSite(slug)
	if err != nil {
		return err
	}

	var url string
	switch {
	case cmd.Console:
		url = fmt.Sprintf("https://app.sandbar.cloud/sites/%s", site.Slug)
	default:
		url = fmt.Sprintf("https://%s.on.sandbar.cloud", site.Slug)
	}

	fmt.Printf("  Opening %s\n", output.Dim.Render(url))
	return openBrowser(url)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}
