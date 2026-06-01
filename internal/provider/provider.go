// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Ensure ghAppProvider satisfies various provider interfaces.
var _ provider.Provider = &ghAppProvider{}

// ghAppProvider defines the provider implementation.
type ghAppProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ghAppProvider{
			version: version,
		}
	}
}

func (p *ghAppProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gh-app"
	resp.Version = p.version
}

func (p *ghAppProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{}
}

func (p *ghAppProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
}

func (p *ghAppProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewExampleResource,
	}
}


func (p *ghAppProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return nil
}
