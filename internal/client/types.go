package client

import "time"

// --- Request types ---

type CreateSiteRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type CreateDeployRequest struct {
	Message      string         `json:"message,omitempty"`
	Branch       string         `json:"branch,omitempty"`
	CommitSHA    string         `json:"commit_sha,omitempty"`
	FileManifest []FileEntry    `json:"file_manifest"`
	Redirects    []RedirectRule `json:"redirect_rules,omitempty"`
	Headers      []HeaderRule   `json:"header_rules,omitempty"`
}

type FileEntry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type AddDomainRequest struct {
	Hostname   string `json:"hostname"`
	RedirectTo string `json:"redirect_to,omitempty"`
}

// UpdateDomainRequest is the body for PATCH /sites/{slug}/domains/{id}.
// Pointer fields distinguish "leave alone" (nil) from "set to empty
// string" (pointer to ""). Today that only matters for RedirectTo —
// clearing a redirect needs to send "".
type UpdateDomainRequest struct {
	RedirectTo *string `json:"redirect_to,omitempty"`
}

type UpdateSiteRequest struct {
	Name             *string `json:"name,omitempty"`
	ProductionBranch *string `json:"production_branch,omitempty"`
	// PreviewExpiry: nil leaves the column alone, empty string
	// clears the override (use platform default), any other value
	// sets it (server parses "7d" / "24h" / etc.).
	PreviewExpiry *string `json:"preview_expiry,omitempty"`
}

type RedirectRule struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status int    `json:"status"`
}

type HeaderRule struct {
	Pattern string            `json:"pattern"`
	Headers map[string]string `json:"headers"`
}

// --- Response types ---

type Site struct {
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	ActiveDeployID   string    `json:"active_deploy_id"`
	ProductionBranch string    `json:"production_branch"`
	PreviewExpiry    string    `json:"preview_expiry,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CreateDeployResponse struct {
	DeployID     string       `json:"deploy_id"`
	Uploads      []UploadInfo `json:"uploads"`
	Skipped      []string     `json:"skipped"`
	SkippedCount int          `json:"skipped_count"`
	UploadCount  int          `json:"upload_count"`
}

type UploadInfo struct {
	Path      string `json:"path"`
	UploadURL string `json:"upload_url"`
}

type Deploy struct {
	ID          string     `json:"id"`
	SiteSlug    string     `json:"site_slug"`
	Status      string     `json:"status"`
	FileCount   int        `json:"file_count"`
	TotalBytes  int64      `json:"total_bytes"`
	Branch      string     `json:"branch,omitempty"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	Message     string     `json:"message"`
	CreatedAt   time.Time  `json:"created_at"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
}

type Domain struct {
	ID                 string     `json:"id"`
	Hostname           string     `json:"hostname"`
	VerificationStatus string     `json:"verification_status"`
	CertificateStatus  string     `json:"certificate_status"`
	RedirectTo         string     `json:"redirect_to,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
}

// Trust is the OIDC deploy trust the Sandbar API returns from Microwave-backed
// trust federation bindings. Identity for reconcile purposes is the (Provider,
// Repository, RefFilter, Environment) tuple; ID is the binding identifier used
// for DELETE.
type Trust struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	Repository  string    `json:"repository"`
	RefFilter   string    `json:"ref_filter"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
}

// AddTrustRequest is the body sent to POST /sites/{slug}/trusts.
// The Sandbar API maps this to the sandbar_github_actions Microwave federation
// key today, so the wire body does not include a provider key. If Sandbar adds
// another deploy-trust federation, add the field back here in lockstep.
type AddTrustRequest struct {
	Repository  string `json:"repository"`
	RefFilter   string `json:"ref_filter,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// AddDomainResponse matches the flat DomainResponse the API returns
// from POST /sites/{slug}/domains. The server embeds the rendered DNS
// instructions as a single string (TrustClaim formats them) rather
// than a structured record-type/name/value triple.
type AddDomainResponse struct {
	Domain
	DNSInstructions string `json:"dns_instructions,omitempty"`
}

type APIError struct {
	Message    string `json:"message"`
	Detail     string `json:"detail"`
	Code       string `json:"code,omitempty"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Message
}

type SearchResponse[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// ListResponse is the unpaged-list envelope used by endpoints that
// return every record in one shot (e.g. GET /sites/{slug}/domains).
// Distinct from SearchResponse — the API serialises these as "items",
// not "data".
type ListResponse[T any] struct {
	Items []T `json:"items"`
}
