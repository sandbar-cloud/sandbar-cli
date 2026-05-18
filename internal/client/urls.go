package client

import "fmt"

// Production hostname constants. If the CLI ever needs to talk to a
// non-prod stack, these become config — but the hostnames the CLI
// renders for users are always the live ones.
const (
	siteDomain    = "on.sandbar.cloud"
	consoleDomain = "app.sandbar.cloud"
)

// LiveURL returns the public URL for a site's default subdomain:
// https://{slug}.on.sandbar.cloud
func LiveURL(slug string) string {
	return fmt.Sprintf("https://%s.%s", slug, siteDomain)
}

// PreviewURL returns the branch-preview URL for a non-production deploy:
// https://{branch}--{slug}.on.sandbar.cloud
func PreviewURL(slug, branch string) string {
	return fmt.Sprintf("https://%s--%s.%s", branch, slug, siteDomain)
}

// ConsoleSiteURL returns the URL of a site's page in the console:
// https://app.sandbar.cloud/sites/{slug}
func ConsoleSiteURL(slug string) string {
	return fmt.Sprintf("https://%s/sites/%s", consoleDomain, slug)
}
