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
		}
		return retryablehttp.DefaultBackoff(minWait, maxWait, attemptNum, resp)
	}

	stdClient := retryClient.StandardClient()
	stdClient.Transport = &etagTransport{transport: stdClient.Transport}
	return stdClient
}

type OrgListResult struct {
	Installations []*github.Installation
	ETag          string
	StatusCode    int
}

type cachedOrgList struct {
	result    OrgListResult
	fetchedAt time.Time
}

type GHClient struct {
	EnterpriseSlug string
	Client         *github.Client

	cacheMu sync.RWMutex
	cache   map[string]cachedOrgList
	sfGroup singleflight.Group
}

const defaultCacheTTL = 5 * time.Second

func (c *GHClient) ListAppInstallationsCached(ctx context.Context, targetOrg, etag string) (OrgListResult, error) {
	cacheKey := fmt.Sprintf("%s:%s", targetOrg, etag)

	c.cacheMu.RLock()
	if cached, ok := c.cache[cacheKey]; ok && time.Since(cached.fetchedAt) < defaultCacheTTL {
		c.cacheMu.RUnlock()
		return cached.result, nil
	}
	c.cacheMu.RUnlock()

	res, err, _ := c.sfGroup.Do(cacheKey, func() (interface{}, error) {
		c.cacheMu.RLock()
		if cached, ok := c.cache[cacheKey]; ok && time.Since(cached.fetchedAt) < defaultCacheTTL {
			c.cacheMu.RUnlock()
			return cached.result, nil
		}
		c.cacheMu.RUnlock()

		reqCtx := ctx
		if etag != "" {
			reqCtx = context.WithValue(ctx, ctxEtagKey, etag)
		}

		installations, resp, err := listAllAppInstallationsRaw(reqCtx, c.Client, c.EnterpriseSlug, targetOrg)

		result := OrgListResult{}
		if resp != nil {
			result.StatusCode = resp.StatusCode
			result.ETag = resp.Header.Get("ETag")
		}

		if err != nil {
			var ghErr *github.ErrorResponse
			if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotModified {
				result.StatusCode = http.StatusNotModified
			} else {
				return OrgListResult{}, err
			}
		} else if result.StatusCode == 0 && resp != nil {
			result.StatusCode = resp.StatusCode
		}

		result.Installations = installations

		c.cacheMu.Lock()
		if c.cache == nil {
			c.cache = make(map[string]cachedOrgList)
		}
		c.cache[cacheKey] = cachedOrgList{
			result:    result,
			fetchedAt: time.Now(),
		}
		c.cacheMu.Unlock()

		return result, nil
	})

	if err != nil {
		return OrgListResult{}, err
	}

	orgResult, ok := res.(OrgListResult)
	if !ok {
		return OrgListResult{}, fmt.Errorf("unexpected result type from singleflight: %T", res)
	}
	return orgResult, nil
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
