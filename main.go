package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/sandbar-cloud/sandbar-cli/cmd"
)

var version = "dev"

type CLI struct {
	Token string `env:"SANDBAR_TOKEN" help:"Auth token." hidden:""`

	Version  cmd.VersionCmd  `cmd:"" help:"Print version."`
	Login    cmd.LoginCmd    `cmd:"" help:"Log in to Sandbar."`
	Init     cmd.InitCmd     `cmd:"" help:"Initialize a Sandbar site in the current directory."`
	Deploy   cmd.DeployCmd   `cmd:"" help:"Deploy the site."`
	Activate cmd.ActivateCmd `cmd:"" help:"Activate a staged deploy."`
	Rollback cmd.RollbackCmd `cmd:"" help:"Roll back to the previous deploy."`
	Domains  cmd.DomainsCmd  `cmd:"" help:"Manage custom domains."`
	Sites    cmd.SitesCmd    `cmd:"" help:"Manage sites."`
	Open     cmd.OpenCmd     `cmd:"" help:"Open site in browser."`
}

func main() {
	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("sandbar"),
		kong.Description("Deploy static sites to Sandbar."),
		kong.UsageOnError(),
	)
	err := ctx.Run(&cmd.Globals{Token: cli.Token, Version: version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
