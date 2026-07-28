// Copyright 2026 Google LLC
// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"gh-app-unofficial": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProvider(t *testing.T) {
	_ = testAccProtoV6ProviderFactories
	testAccPreCheck(t)
}

func TestProvider_Unit_Configure(t *testing.T) {
	cases := []struct {
		name    string
		token   types.String
		slug    types.String
		baseURL types.String
		env     string
		wantErr string
	}{
		{
			name:    "unknown_token",
			token:   types.StringUnknown(),
			slug:    types.StringNull(),
			baseURL: types.StringNull(),
			env:     "",
			wantErr: "Unknown API Token",
		},
		{
			name:    "unknown_enterprise",
			token:   types.StringValue("mock-token"),
			slug:    types.StringUnknown(),
			baseURL: types.StringNull(),
			env:     "",
			wantErr: "Unknown Enterprise Slug",
		},
		{
			name:    "unknown_base_url",
			token:   types.StringValue("mock-token"),
			slug:    types.StringValue("mock-ent"),
			baseURL: types.StringUnknown(),
			env:     "",
			wantErr: "Unknown Base URL",
		},
		{
			name:    "missing_token_and_slug",
			token:   types.StringNull(),
			slug:    types.StringNull(),
			baseURL: types.StringNull(),
			env:     "",
			wantErr: "Missing API Token",
		},
		{
			name:    "invalid_base_url",
			token:   types.StringValue("mock-token"),
			slug:    types.StringValue("mock-ent"),
			baseURL: types.StringValue("://invalid-url"),
			env:     "",
			wantErr: "missing protocol scheme",
		},
		{
			name:    "valid_config",
			token:   types.StringValue("mock-token"),
			slug:    types.StringValue("mock-ent"),
			baseURL: types.StringNull(),
			env:     "",
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			p := &GHAppProvider{}

			if tc.env != "" {
				t.Setenv("GITHUB_TOKEN", tc.env)
			} else {
				t.Setenv("GITHUB_TOKEN", "")
			}

			var schemaResp provider.SchemaResponse
			p.Schema(ctx, provider.SchemaRequest{}, &schemaResp)

			model := ghProviderModel{
				EnterpriseSlug: tc.slug,
				Token:          tc.token,
				BaseURL:        tc.baseURL,
			}
			req := provider.ConfigureRequest{
				Config: newTestConfig(t, ctx, schemaResp.Schema, model),
			}

			resp := &provider.ConfigureResponse{}

			p.Configure(ctx, req, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
		})
	}
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		return
	}

	if os.Getenv("GITHUB_TOKEN") == "" || os.Getenv("GITHUB_ENTERPRISE_SLUG") == "" || os.Getenv("GITHUB_TARGET_ORG") == "" || os.Getenv("GITHUB_APP_CLIENT_ID") == "" {
		t.Fatal("GITHUB_TOKEN, GITHUB_ENTERPRISE_SLUG, GITHUB_TARGET_ORG, and GITHUB_APP_CLIENT_ID must be set for acceptance tests.")
	}
}

// TestProvider_Unit_MetadataAndFactories verifies that the provider constructor (New),
// Metadata response, and resource/data-source factory registration return valid framework components.
func TestProvider_Unit_MetadataAndFactories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	p := New("1.0.0")()

	var metaResp provider.MetadataResponse
	p.Metadata(ctx, provider.MetadataRequest{}, &metaResp)
	if got, want := metaResp.TypeName, "ghapp"; got != want {
		t.Errorf("Metadata() TypeName = %q, want %q", got, want)
	}
	if got, want := metaResp.Version, "1.0.0"; got != want {
		t.Errorf("Metadata() Version = %q, want %q", got, want)
	}

	resources := p.Resources(ctx)
	if len(resources) == 0 || resources[0]() == nil {
		t.Error("Resources() returned empty or nil factory slice")
	}

	dataSources := p.DataSources(ctx)
	if len(dataSources) == 0 || dataSources[0]() == nil {
		t.Error("DataSources() returned empty or nil factory slice")
	}
}
