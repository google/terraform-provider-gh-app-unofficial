// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/oauth2"

	"github.com/google/go-github/v88/github"
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
	resp.TypeName = "ghapp"
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
		},
	}
}

type ghProviderModel struct {
	Token          types.String `tfsdk:"token"`
	EnterpriseSlug types.String `tfsdk:"enterprise_slug"`
}

type GHClient struct {
	EnterpriseSlug string
	Client         *github.Client
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

	if resp.Diagnostics.HasError() {
		return
	}

	token := os.Getenv("GITHUB_TOKEN")
	entpriseSlug := config.EnterpriseSlug.ValueString()

	// configuration takes precedence over environment variable
	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	// validation
	if token == "" {
		resp.Diagnostics.AddError("Missing API Token", "The token attribute must be set.")
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
	})

	// initialize the client
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	ghClient, err := github.NewClient(github.WithHTTPClient(oauth2.NewClient(ctx, ts)))
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
			return &InstallationsDataSource{}
		},
	}
}
