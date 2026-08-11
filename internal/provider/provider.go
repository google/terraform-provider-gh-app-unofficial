// Copyright 2026 Google LLC
// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/sync/singleflight"
)

// Ensure GHAppProvider satisfies various provider interfaces.
var _ provider.Provider = &GHAppProvider{}

// GHAppProvider defines the provider implementation.
type GHAppProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GHAppProvider{
			version: version,
		}
	}
}

func (p *GHAppProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gh-app-unofficial"
	resp.Version = p.version
}

func (p *GHAppProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Terraform provider for managing GitHub App installations and repository access topologies across GitHub Enterprise organizations.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The GitHub App Installation Access Token. Can also be set via GITHUB_TOKEN environment variable.",
			},
			"enterprise_slug": schema.StringAttribute{
				Required:    true,
				Description: "The URL-friendly slug of the GitHub Enterprise account.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "The GitHub Enterprise Server or custom API Base URL. Defaults to `https://api.github.com/`. Can also be set via GITHUB_BASE_URL, GITHUB_ENTERPRISE_BASE_URL, or GITHUB_API_URL environment variables.",
			},
		},
	}
}

type ghProviderModel struct {
	Token          types.String `tfsdk:"token"`
	EnterpriseSlug types.String `tfsdk:"enterprise_slug"`
	BaseURL        types.String `tfsdk:"base_url"`
}

type ctxEtagKeyType struct{}

var ctxEtagKey = ctxEtagKeyType{}

// etagTransport is an http.RoundTripper that injects an If-None-Match header
// when an ETag string is present in the request context under ctxEtagKey.
//
// Architectural Note: We use a custom transport paired with orgInstallationCache rather than
// a generic HTTP cache transport (such as github-conditional-http-transport) because:
//  1. We require organization-scoped cache invalidation (InvalidateOrgCache) immediately
//     after mutative write operations (read-after-write consistency).
//  2. We combine a 5s in-memory TTL struct cache with singleflight request coalescing
//     across concurrent Terraform resource reads to achieve 0-network sub-millisecond reads.
//  3. On HTTP 304 Not Modified, we return the parsed Go structs directly with 0 JSON
//     deserialization overhead.
type etagTransport struct {
	transport http.RoundTripper
}

func (t *etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	baseTransport := t.transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	if etag, ok := req.Context().Value(ctxEtagKey).(string); ok && etag != "" {
		// Clone request to avoid mutating caller's request per http.RoundTripper contract.
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", etag)
	}
	return baseTransport.RoundTrip(req)
}

func newRetryableHTTPClient() *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 30 * time.Second

	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if resp != nil {
			if resp.StatusCode == http.StatusTooManyRequests {
				return true, nil
			}
			if resp.StatusCode == http.StatusForbidden {
				if resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0" {
					return true, nil
				}
			}
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}

	retryClient.Backoff = func(minWait, maxWait time.Duration, attemptNum int, resp *http.Response) time.Duration {
		if resp != nil {
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil && seconds > 0 {
					return time.Duration(seconds) * time.Second
				}
			}
			if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
				if resetUnix, parseErr := strconv.ParseInt(reset, 10, 64); parseErr == nil {
					wait := time.Until(time.Unix(resetUnix, 0))
					if wait > 0 && wait <= maxWait {
						return wait
					}
				}
			}
		}
		return retryablehttp.DefaultBackoff(minWait, maxWait, attemptNum, resp)
	}

	stdClient := retryClient.StandardClient()
	stdClient.Transport = &etagTransport{transport: stdClient.Transport}
	return stdClient
}

const defaultCacheTTL = 5 * time.Second

type cacheEntry struct {
	installations []*github.Installation
	etag          string
	fetchedAt     time.Time
}

type orgInstallationCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]cacheEntry
}

func newOrgInstallationCache(ttl time.Duration) *orgInstallationCache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &orgInstallationCache{
		ttl:     ttl,
		entries: make(map[string]cacheEntry),
	}
}

// cloneInstallation creates a deep defensive copy of a github.Installation struct
// and all nested pointer/slice fields via JSON round-trip.
func cloneInstallation(in *github.Installation) *github.Installation {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out github.Installation
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return &out
}

// cloneInstallations returns a deep defensive copy of a slice of installation pointers.
func cloneInstallations(in []*github.Installation) []*github.Installation {
	if in == nil {
		return nil
	}
	out := make([]*github.Installation, len(in))
	for i, inst := range in {
		out[i] = cloneInstallation(inst)
	}
	return out
}

// Get returns the cached installations and ETag for an organization.
// isFresh is true if the cached entry is within the TTL window.
// exists is true if an entry is present (even if expired).
// The returned slice is defensively cloned to ensure callers cannot mutate internal cache state.
func (c *orgInstallationCache) Get(org string) (entry cacheEntry, isFresh bool, exists bool) {
	if c == nil {
		return cacheEntry{}, false, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.entries == nil {
		return cacheEntry{}, false, false
	}
	entry, exists = c.entries[org]
	if !exists {
		return cacheEntry{}, false, false
	}
	isFresh = time.Since(entry.fetchedAt) < c.ttl
	entry.installations = cloneInstallations(entry.installations)
	return entry, isFresh, true
}

// Set stores or updates cached installations and ETag for an organization.
// The provided slice is defensively cloned to isolate the cache from caller modifications.
func (c *orgInstallationCache) Set(org string, installations []*github.Installation, etag string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[string]cacheEntry)
	}
	c.entries[org] = cacheEntry{
		installations: cloneInstallations(installations),
		etag:          etag,
		fetchedAt:     time.Now(),
	}
}

// Touch refreshes fetchedAt for an existing cache entry when GitHub returns HTTP 304 Not Modified.
func (c *orgInstallationCache) Touch(org string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries != nil {
		if entry, exists := c.entries[org]; exists {
			entry.fetchedAt = time.Now()
			c.entries[org] = entry
		}
	}
}

// Invalidate clears the cached entry for an organization upon mutative operations.
func (c *orgInstallationCache) Invalidate(org string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries != nil {
		delete(c.entries, org)
	}
}

// Expire sets fetchedAt to zero time to simulate TTL expiration in unit tests.
func (c *orgInstallationCache) Expire(org string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries != nil {
		if entry, exists := c.entries[org]; exists {
			entry.fetchedAt = time.Time{}
			c.entries[org] = entry
		}
	}
}

type GHClient struct {
	EnterpriseSlug string
	Client         *github.Client

	cache     *orgInstallationCache
	cacheOnce sync.Once
	sfGroup   singleflight.Group
}

func (c *GHClient) getCache() *orgInstallationCache {
	c.cacheOnce.Do(func() {
		if c.cache == nil {
			c.cache = newOrgInstallationCache(defaultCacheTTL)
		}
	})
	return c.cache
}

func (c *GHClient) ListAppInstallationsCached(ctx context.Context, targetOrg string) ([]*github.Installation, error) {
	cache := c.getCache()

	// 1. Return in-memory cached list if within TTL (defensively cloned in cache.Get)
	cached, isFresh, hasCache := cache.Get(targetOrg)
	if isFresh {
		return cached.installations, nil
	}

	// 2. Coalesce concurrent requests & send in-memory ETag for conditional GET
	resVal, err, _ := c.sfGroup.Do(targetOrg, func() (interface{}, error) {
		// Re-check cache inside singleflight in case a concurrent worker just populated it
		if current, fresh, _ := cache.Get(targetOrg); fresh {
			return current.installations, nil
		}

		reqCtx := ctx
		if hasCache && cached.etag != "" {
			reqCtx = context.WithValue(ctx, ctxEtagKey, cached.etag)
		}

		installations, resp, err := listAllAppInstallationsRaw(reqCtx, c.Client, c.EnterpriseSlug, targetOrg)

		// 3. Handle 304 Not Modified (0 rate limit cost!)
		var ghErr *github.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotModified {
			cache.Touch(targetOrg)
			if touched, _, found := cache.Get(targetOrg); found && touched.installations != nil {
				return touched.installations, nil
			}
			return cloneInstallations(cached.installations), nil
		}

		if err != nil {
			return nil, err
		}

		newETag := ""
		if resp != nil {
			newETag = resp.Header.Get("ETag")
		}

		cache.Set(targetOrg, installations, newETag)
		return cloneInstallations(installations), nil
	})

	if err != nil {
		return nil, err
	}

	instList, ok := resVal.([]*github.Installation)
	if !ok {
		return nil, fmt.Errorf("unexpected result type from singleflight: %T", resVal)
	}

	return cloneInstallations(instList), nil
}

func (c *GHClient) InvalidateOrgCache(targetOrg string) {
	c.getCache().Invalidate(targetOrg)
}

func (p *GHAppProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config ghProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddError("Unknown API Token", "The provider cannot evaluate the GitHub API token.")
	}

	if config.EnterpriseSlug.IsUnknown() {
		resp.Diagnostics.AddError("Unknown Enterprise Slug", "The provider cannot evaluate the enterprise slug.")
	}

	if config.BaseURL.IsUnknown() {
		resp.Diagnostics.AddError("Unknown Base URL", "The provider cannot evaluate the base URL.")
	}

	if resp.Diagnostics.HasError() {
		return
	}

	token := os.Getenv("GITHUB_TOKEN")
	entpriseSlug := config.EnterpriseSlug.ValueString()
	baseURL := os.Getenv("GITHUB_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("GITHUB_ENTERPRISE_BASE_URL")
	}
	if baseURL == "" {
		baseURL = os.Getenv("GITHUB_API_URL")
	}

	// configuration takes precedence over environment variable
	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	if !config.BaseURL.IsNull() {
		baseURL = config.BaseURL.ValueString()
	}

	// validation
	if token == "" {
		resp.Diagnostics.AddError("Missing API Token", "The token attribute or GITHUB_TOKEN environment variable must be set.")
	}

	if entpriseSlug == "" {
		resp.Diagnostics.AddError("Missing Enterprise Slug", "The enterprise_slug attribute must be set.")
	}

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Configuring GitHub client settings", map[string]interface{}{
		"enterprise_slug": entpriseSlug,
		"token_set":       token != "",
		"base_url":        baseURL,
	})

	// initialize the client
	httpClient := newRetryableHTTPClient()
	clientOpts := []github.ClientOptionsFunc{
		github.WithAuthToken(token),
		github.WithHTTPClient(httpClient),
	}

	if baseURL != "" {
		baseURL = formatBaseURL(baseURL, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		baseURL = "https://api.github.com/"
	}

	clientOpts = append(clientOpts, github.WithURLs(&baseURL, nil))

	ghClient, err := github.NewClient(clientOpts...)

	if err != nil {
		tflog.Error(ctx, "Failed to create GitHub client", map[string]interface{}{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create GitHub client: %s", err))
		return
	}

	client := &GHClient{
		EnterpriseSlug: entpriseSlug,
		Client:         ghClient,
		cache:          newOrgInstallationCache(defaultCacheTTL),
	}

	// make the data available to data sources and resources
	resp.DataSourceData = client
	resp.ResourceData = client

	tflog.Info(ctx, "Configured client", map[string]interface{}{
		"enterprise_slug": entpriseSlug,
	})
}

func (p *GHAppProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource {
			return &installationResource{}
		},
	}
}

func (p *GHAppProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource {
			return &installationsDataSource{}
		},
	}
}
