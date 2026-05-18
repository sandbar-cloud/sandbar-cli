# sandbar-cli

Command-line interface for deploying static sites to [Sandbar](https://sandbar.cloud).

---

## Installation

### Homebrew (macOS and Linux)

```sh
brew install sandbar-cloud/tap/sandbar
```

### From releases

Download a pre-built binary from the [releases page](https://github.com/mataki-dev/sandbar-cli/releases). Binaries are available for macOS and Linux on amd64 and arm64.

```sh
# Example: macOS arm64
curl -L https://github.com/mataki-dev/sandbar-cli/releases/latest/download/sandbar_latest_darwin_arm64.tar.gz | tar xz
mv sandbar /usr/local/bin/sandbar
```

### From source

Requires Go 1.26 or later.

```sh
go install github.com/mataki-dev/sandbar-cli@latest
```

### Build locally

```sh
git clone https://github.com/mataki-dev/sandbar-cli.git
cd sandbar-cli
make build
# Produces ./sandbar
```

---

## Quick Start

```sh
sandbar login          # Authenticate via browser
sandbar init           # Configure the current directory as a Sandbar site
sandbar deploy         # Build and deploy
```

---

## Commands

### `sandbar version`

Print the CLI version.

```sh
sandbar version
```

---

### `sandbar login`

Authenticate with Sandbar. Behavior depends on context:

- **Local:** opens a browser for the device authorization flow, then polls for approval. The token is saved to `~/.config/sandbar/config.toml`.
- **GitHub Actions:** automatically exchanges a GitHub OIDC token with the Sandbar API. No flags required; the command detects the `ACTIONS_ID_TOKEN_REQUEST_URL` environment variable.

```sh
sandbar login
```

No flags.

---

### `sandbar init`

Initialize a Sandbar site in the current directory. Creates `.sandbar/config.toml`. No API call is made; the site is created on the first deploy.

```sh
sandbar init [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--name` | `-n` | Site name (defaults to the directory name) |
| `--dir` | `-d` | Build output directory |
| `--yes` | `-y` | Accept all defaults without prompting |

The command attempts to detect the build framework from the directory structure:

| Directory | Detected framework |
|-----------|--------------------|
| `dist/`   | Astro              |
| `public/` | Hugo               |
| `build/`  | Create React App   |
| `out/`    | Next.js            |

If none of these exist, `dist` is used as the default.

**Examples:**

```sh
# Interactive
sandbar init

# Non-interactive, accept defaults
sandbar init --yes

# Specify name and build directory
sandbar init --name my-site --dir out
```

---

### `sandbar deploy`

Deploy the site. On the first deploy, the site is created in Sandbar and the assigned site ID is written back to `.sandbar/config.toml`. Subsequent deploys skip site creation.

The deploy process:
1. Hashes all files in the build directory.
2. Sends the manifest to the API, which returns only the files that need uploading.
3. Uploads new or changed files in parallel.
4. Finalizes the deploy and waits for the content safety scan to complete.
5. Activates the deploy (unless `--no-activate` is set).

```sh
sandbar deploy [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--dir` | `-d` | from config or `dist` | Build output directory |
| `--no-activate` | | false | Upload without activating (staged deploy) |
| `--message` | `-m` | git HEAD message | Deploy message |
| `--concurrency` | `-c` | `8` | Number of parallel upload workers |

**Examples:**

```sh
# Standard deploy
sandbar deploy

# Deploy without activating (stage it first)
sandbar deploy --no-activate

# Deploy with a custom message and 16 parallel workers
sandbar deploy --message "Release v2.1" --concurrency 16

# Deploy from a non-default build directory
sandbar deploy --dir out
```

---

### `sandbar preview`

Deploy to a temporary preview URL that expires automatically. Useful for branch-based previews in CI. The preview label defaults to the current git branch name.

```sh
sandbar preview [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--label` | `-l` | git branch name | Preview label |
| `--expires` | `-e` | from config or `7d` | Expiry duration (e.g. `48h`, `7d`, `30d`) |
| `--dir` | `-d` | from config or `dist` | Build output directory |

**Examples:**

```sh
# Preview with defaults (label = branch name, expiry = 7d)
sandbar preview

# Preview with custom label and expiry
sandbar preview --label "pr-123" --expires 48h
```

---

### `sandbar activate`

Activate a previously staged deploy by its deploy ID.

```sh
sandbar activate <deploy-id>
```

**Example:**

```sh
sandbar activate dep_abc123
```

---

### `sandbar rollback`

Roll back the site to the most recent superseded deploy. Prompts for confirmation unless `--yes` is passed.

```sh
sandbar rollback [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--yes` | `-y` | Skip confirmation prompt |

**Examples:**

```sh
sandbar rollback
sandbar rollback --yes
```

---

### `sandbar open`

Open the site in the default browser.

```sh
sandbar open [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--preview` | `-p` | Open the most recent preview URL |
| `--console` | `-c` | Open the Sandbar console for this site |

**Examples:**

```sh
# Open live site
sandbar open

# Open most recent preview
sandbar open --preview

# Open the console
sandbar open --console
```

---

### `sandbar sites`

Manage sites.

#### `sandbar sites list`

List all sites in the account, with their ID, slug, live URL, and time of last deploy.

```sh
sandbar sites list
```

#### `sandbar sites info`

Show details for the site in the current directory, including active deploy, custom domains, SSL status, and total deploy count.

```sh
sandbar sites info
```

#### `sandbar sites update`

Update the site in the current directory.

```sh
sandbar sites update [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--name` | `-n` | New display name |
| `--production-branch` | | Production branch name |

At least one flag is required.

#### `sandbar sites delete`

Delete the site in the current directory. Prompts for confirmation by asking you to retype the slug.

```sh
sandbar sites delete [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--yes` | `-y` | Skip confirmation |

---

### `sandbar domains`

Manage custom domains for the site in the current directory.

#### `sandbar domains add <hostname>`

Add a custom domain. Prints the DNS record required to verify ownership.

```sh
sandbar domains add example.com
```

After adding the DNS record, run `sandbar domains verify` to check propagation.

#### `sandbar domains list`

List all domains on the site, with verification status and SSL status.

```sh
sandbar domains list
```

#### `sandbar domains verify <hostname>`

Re-check verification status for a domain.

```sh
sandbar domains verify example.com
```

#### `sandbar domains delete <hostname>`

Delete a custom domain. Prompts for confirmation.

```sh
sandbar domains delete example.com [flags]
```

| Flag    | Short | Description       |
|---------|-------|-------------------|
| `--yes` | `-y`  | Skip confirmation |

---

## Authentication

### Local device flow

Running `sandbar login` locally opens a browser to the Sandbar authorization page. The CLI polls until the request is approved, then saves the token to:

```
~/.config/sandbar/config.toml
```

The token is written to `[auth] token` in that file.

### GitHub Actions OIDC (CI)

When `ACTIONS_ID_TOKEN_REQUEST_URL` is set (standard in GitHub Actions runners), `sandbar login` skips the device flow and exchanges a GitHub OIDC JWT directly with the Sandbar API. No stored secrets are required beyond granting the `id-token: write` permission.

See the [GitHub Actions](#github-actions) section for a complete workflow example.

### Token via environment variable

You can pass a token directly without running `sandbar login`:

```sh
SANDBAR_TOKEN=your_token sandbar deploy
```

The environment variable takes priority over the stored token.

### Token resolution order

1. `--token` flag (global, hidden)
2. `SANDBAR_TOKEN` environment variable
3. `~/.config/sandbar/config.toml` → `[auth] token`

---

## Configuration

### Project config: `.sandbar/config.toml`

Created by `sandbar init` in the project root. Commit this file to version control; the `id` field is written automatically on first deploy.

```toml
[site]
id        = "site_abc123"   # Written automatically on first deploy
name      = "my-site"
build_dir = "dist"
framework = "astro"         # Informational; set by auto-detection

[deploy]
auto_activate   = true   # Activate the deploy immediately after upload
message_from_git = true  # Use the git HEAD commit message as the deploy message

[preview]
default_expiry = "7d"    # Default expiry for preview deploys (e.g. 48h, 7d, 30d)

# Optional: redirect rules (Netlify-compatible)
[[redirects]]
from   = "/old-path"
to     = "/new-path"
status = 301

[[redirects]]
from   = "/docs"
to     = "https://docs.example.com"
status = 302

# Optional: custom response headers (Netlify-compatible)
[[headers]]
for = "/*"
[headers.values]
X-Frame-Options        = "DENY"
X-Content-Type-Options = "nosniff"

[[headers]]
for = "/assets/*"
[headers.values]
Cache-Control = "public, max-age=31536000, immutable"
```

**Field reference:**

| Field | Type | Description |
|-------|------|-------------|
| `site.id` | string | Site ID, written automatically on first deploy |
| `site.name` | string | Human-readable site name |
| `site.build_dir` | string | Relative path to the build output directory |
| `site.framework` | string | Framework name, set by auto-detection during `init` |
| `deploy.auto_activate` | bool | Activate immediately after upload (default: `true`) |
| `deploy.message_from_git` | bool | Use git HEAD message as deploy message (default: `true`) |
| `preview.default_expiry` | string | Default expiry duration for preview URLs (default: `7d`) |
| `redirects` | array | Redirect rules (`[[redirects]]`); Netlify-compatible syntax |
| `redirects[].from` | string | Source path or pattern (supports `/*` splat) |
| `redirects[].to` | string | Destination URL or path (supports `:splat`) |
| `redirects[].status` | int | HTTP status code (`301`, `302`) |
| `redirects[].force` | bool | Force redirect even if path exists (default: `false`) |
| `headers` | array | Response header rules (`[[headers]]`); Netlify-compatible syntax |
| `headers[].for` | string | Path pattern to match |
| `headers[].values` | map | Header key-value pairs |

### Global config: `~/.config/sandbar/config.toml`

Managed by `sandbar login`. You can also edit it directly.

```toml
[auth]
token = "sbk_live_..."

# Optional: override the API base URL (for self-hosted or staging)
api_url = "https://api.sandbar.cloud"
```

**Field reference:**

| Field | Type | Description |
|-------|------|-------------|
| `auth.token` | string | Session token |
| `api_url` | string | API base URL override (default: `https://api.sandbar.cloud`) |

### Environment variables

| Variable | Description |
|----------|-------------|
| `SANDBAR_TOKEN` | Auth token; takes priority over the stored token |
| `SANDBAR_API_URL` | API base URL override; takes priority over `api_url` in config |

---

## GitHub Actions

The CLI integrates with GitHub Actions via OIDC. No secrets need to be stored; the runner exchanges a short-lived JWT automatically.

### Requirements

- Add `permissions: id-token: write` to the job.
- The Sandbar project must be configured to trust your repository (set up in the Sandbar console).

### Example workflow

```yaml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write   # Required for GitHub OIDC

    steps:
      - uses: actions/checkout@v4

      - name: Install sandbar
        run: |
          curl -L https://github.com/mataki-dev/sandbar-cli/releases/latest/download/sandbar_latest_linux_amd64.tar.gz | tar xz
          sudo mv sandbar /usr/local/bin/sandbar

      - name: Build
        run: npm ci && npm run build

      - name: Deploy
        run: |
          sandbar login
          sandbar deploy
```

### Preview deploys on pull requests

```yaml
name: Preview

on:
  pull_request:

jobs:
  preview:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write

    steps:
      - uses: actions/checkout@v4

      - name: Install sandbar
        run: |
          curl -L https://github.com/mataki-dev/sandbar-cli/releases/latest/download/sandbar_latest_linux_amd64.tar.gz | tar xz
          sudo mv sandbar /usr/local/bin/sandbar

      - name: Build
        run: npm ci && npm run build

      - name: Preview
        run: |
          sandbar login
          sandbar preview --label "pr-${{ github.event.pull_request.number }}" --expires 7d
```

---

## Project Structure

```
sandbar-cli/
├── main.go                   # Entry point; CLI struct and kong wiring
├── cmd/
│   ├── globals.go            # Shared Globals type (API client, config helpers)
│   ├── login.go              # sandbar login (device flow + GitHub OIDC)
│   ├── init.go               # sandbar init
│   ├── deploy.go             # sandbar deploy
│   ├── preview.go            # sandbar preview
│   ├── activate.go           # sandbar activate
│   ├── rollback.go           # sandbar rollback
│   ├── open.go               # sandbar open
│   ├── sites.go              # sandbar sites list / info
│   ├── domains.go            # sandbar domains add / list / verify
│   └── version.go            # sandbar version
├── internal/
│   ├── client/               # Sandbar API client
│   │   └── client.go
│   ├── config/               # Config file reading and writing
│   │   └── config.go
│   ├── git/                  # Git helpers (branch name, HEAD message)
│   │   └── git.go
│   ├── hasher/               # File hashing for deploy manifests
│   │   └── hasher.go
│   ├── output/               # Terminal output helpers (spinner, table, styles)
│   │   └── output.go
│   └── uploader/             # Parallel file upload to signed URLs
│       └── uploader.go
├── .goreleaser.yaml          # Release configuration (GitHub, Homebrew)
├── Makefile
├── go.mod
└── go.sum
```

---

## Building

```sh
# Development build
make build

# Run tests
make test

# Lint (requires golangci-lint)
make lint

# Clean build artifacts
make clean

# Build with explicit version
make build VERSION=1.2.3
```

The binary embeds the version string at link time via `-X main.version=$(VERSION)`. The version is included in the `User-Agent` header sent to the API.
