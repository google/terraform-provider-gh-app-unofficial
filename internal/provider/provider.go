// Copyright 2026 Google LLC
// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
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

type etagTransport struct {
	transport http.RoundTripper
}

func (t *etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if etag, ok := req.Context().Value(ctxEtagKey).(string); ok && etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	baseTransport := t.transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
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

type cachedOrgList struct {
	installations []*github.Installation
	etag          string
	fetchedAt     time.Time
}

type GHClient struct {
	EnterpriseSlug string
	Client         *github.Client

	cacheMu sync.RWMutex
	cache   map[string]cachedOrgList
	sfGroup singleflight.Group
}

const defaultCacheTTL = 5 * time.Second

func (c *GHClient) ListAppInstallationsCached(ctx context.Context, targetOrg string) ([]*github.Installation, error) {
	// 1. Return in-memory cached list if within 5s TTL
	c.cacheMu.RLock()
	cached, hasCache := c.cache[targetOrg]
	if hasCache && time.Since(cached.fetchedAt) < defaultCacheTTL {
		c.cacheMu.RUnlock()
		return cached.installations, nil
	}
	c.cacheMu.RUnlock()

	// 2. Coalesce concurrent requests & send in-memory ETag for conditional GET
	resVal, err, _ := c.sfGroup.Do(targetOrg, func() (interface{}, error) {
		reqCtx := ctx
		if hasCache && cached.etag != "" {
			reqCtx = context.WithValue(ctx, ctxEtagKey, cached.etag)
		}

		installations, resp, err := listAllAppInstallationsRaw(reqCtx, c.Client, c.EnterpriseSlug, targetOrg)

		// 3. Handle 304 Not Modified (0 rate limit cost!)
		var ghErr *github.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotModified {
			c.cacheMu.Lock()
			c.cache[targetOrg] = cachedOrgList{
				installations: cached.installations,
				etag:          cached.etag,
				fetchedAt:     time.Now(),
			}
			c.cacheMu.Unlock()
			return cached.installations, nil
		}

		if err != nil {
			return nil, err
		}

		newETag := ""
		if resp != nil {
			newETag = resp.Header.Get("ETag")
		}

		c.cacheMu.Lock()
		if c.cache == nil {
			c.cache = make(map[string]cachedOrgList)
		}
		c.cache[targetOrg] = cachedOrgList{
			installations: installations,
			etag:          newETag,
			fetchedAt:     time.Now(),
		}
		c.cacheMu.Unlock()

		return installations, nil
	})

	if err != nil {
		return nil, err
	}

	instList, ok := resVal.([]*github.Installation)
	if !ok {
		return nil, fmt.Errorf("unexpected result type from singleflight: %T", resVal)
	}

	return instList, nil
}

func (c *GHClient) InvalidateOrgCache(targetOrg string) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.cache != nil {
		delete(c.cache, targetOrg)
	}
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
