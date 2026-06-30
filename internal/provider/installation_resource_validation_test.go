package provider

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestInstallationResource_ValidateConfig_Unit performs pure Go unit testing on the ValidateConfig method.
// It directly executes r.ValidateConfig without requiring Terraform CLI or API access.
func TestInstallationResource_ValidateConfig_Unit(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		repoSelection types.String
		selectedRepos types.List
		wantErr       string
	}{
		"unknown_repo_selection": {
			repoSelection: types.StringUnknown(),
			selectedRepos: types.ListNull(types.StringType),
			wantErr:       "",
		},
		"null_repo_selection_with_null_repos": {
			repoSelection: types.StringNull(),
			selectedRepos: types.ListNull(types.StringType),
			wantErr:       "",
		},
		"null_repo_selection_with_repos": {
			repoSelection: types.StringNull(),
			selectedRepos: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("repo-1")}),
			wantErr:       "Invalid Selected Repositories",
		},
		"selected_selection_missing_repos": {
			repoSelection: types.StringValue("selected"),
			selectedRepos: types.ListNull(types.StringType),
			wantErr:       "Missing Selected Repositories",
		},
		"selected_selection_empty_repos": {
			repoSelection: types.StringValue("selected"),
			selectedRepos: types.ListValueMust(types.StringType, []attr.Value{}),
			wantErr:       "Empty Selected Repositories",
		},
		"selected_selection_unknown_repos": {
			repoSelection: types.StringValue("selected"),
			selectedRepos: types.ListUnknown(types.StringType),
			wantErr:       "",
		},
		"selected_selection_valid_repos": {
			repoSelection: types.StringValue("selected"),
			selectedRepos: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("repo-1")}),
			wantErr:       "",
		},
		"all_selection_with_repos": {
			repoSelection: types.StringValue("all"),
			selectedRepos: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("repo-1")}),
			wantErr:       "Invalid Selected Repositories",
		},
		"all_selection_null_repos": {
			repoSelection: types.StringValue("all"),
			selectedRepos: types.ListNull(types.StringType),
			wantErr:       "",
		},
		"all_selection_empty_repos": {
			repoSelection: types.StringValue("all"),
			selectedRepos: types.ListValueMust(types.StringType, []attr.Value{}),
			wantErr:       "",
		},
		"all_selection_unknown_repos": {
			repoSelection: types.StringValue("all"),
			selectedRepos: types.ListUnknown(types.StringType),
			wantErr:       "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			r := &installationResource{}

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			model := installationResourceModel{
				ID:                   types.StringNull(),
				TargetOrg:            types.StringValue("test-org"),
				ClientID:             types.StringValue("test-client-id"),
				AppSlug:              types.StringNull(),
				SelectedRepositories: tc.selectedRepos,
				RepositorySelection:  tc.repoSelection,
				Events:               types.ListNull(types.StringType),
				Permissions:          types.MapNull(types.StringType),
				CreatedAt:            types.StringNull(),
				UpdatedAt:            types.StringNull(),
			}

			var objVal attr.Value
			diags := tfsdk.ValueFrom(ctx, model, schemaResp.Schema.Type(), &objVal)
			if diags.HasError() {
				t.Fatalf("ValueFrom error: %v", diags)
			}

			rawVal, err := objVal.ToTerraformValue(ctx)
			if err != nil {
				t.Fatalf("ToTerraformValue error: %v", err)
			}

			config := tfsdk.Config{
				Raw:    rawVal,
				Schema: schemaResp.Schema,
			}

			req := resource.ValidateConfigRequest{
				Config: config,
			}
			var resp resource.ValidateConfigResponse

			r.ValidateConfig(ctx, req, &resp)

			if tc.wantErr != "" {
				if !resp.Diagnostics.HasError() {
					t.Fatalf("expected error containing %q, got no error", tc.wantErr)
				}
				found := false
				for _, diag := range resp.Diagnostics.Errors() {
					if strings.Contains(diag.Summary(), tc.wantErr) || strings.Contains(diag.Detail(), tc.wantErr) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected error containing %q, got diagnostics: %v", tc.wantErr, resp.Diagnostics)
				}
			} else {
				if resp.Diagnostics.HasError() {
					t.Fatalf("expected no error, got diagnostics: %v", resp.Diagnostics)
				}
			}
		})
	}
}

// TestInstallationResourceValidation performs offline HCL schema and configuration validation using resource.UnitTest.
func TestInstallationResourceValidation(t *testing.T) {
	t.Parallel()

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			// Case 1: repository_selection is 'selected' but selected_repositories is omitted (Null)
			{
				Config:      testAccInstallationResourceValidationConfig_missingRepos,
				ExpectError: regexp.MustCompile("Missing Selected Repositories"),
			},
			// Case 2: repository_selection is 'selected' but selected_repositories is empty ([])
			{
				Config:      testAccInstallationResourceValidationConfig_emptyRepos,
				ExpectError: regexp.MustCompile("Empty Selected Repositories"),
			},
			// Case 3: repository_selection is 'all' but selected_repositories is provided
			{
				Config:      testAccInstallationResourceValidationConfig_invalidAllRepos,
				ExpectError: regexp.MustCompile("Invalid Selected Repositories"),
			},
			// Case 4: repository_selection omitted (defaults to 'all') but selected_repositories is provided
			{
				Config:      testAccInstallationResourceValidationConfig_defaultAllInvalidRepos,
				ExpectError: regexp.MustCompile("Invalid Selected Repositories"),
			},
			// Case 5: Valid configuration with repository_selection = "all" (PlanOnly)
			{
				Config:             testAccInstallationResourceValidationConfig_validAll,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Case 6: Valid configuration with repository_selection = "selected" (PlanOnly)
			{
				Config:             testAccInstallationResourceValidationConfig_validSelected,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

const testAccInstallationResourceValidationConfig_missingRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org           = "test-org"
  client_id            = "test-client-id"
  repository_selection = "selected"
}
`

const testAccInstallationResourceValidationConfig_emptyRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org            = "test-org"
  client_id             = "test-client-id"
  repository_selection  = "selected"
  selected_repositories = []
}
`

const testAccInstallationResourceValidationConfig_invalidAllRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org            = "test-org"
  client_id             = "test-client-id"
  repository_selection  = "all"
  selected_repositories = ["repo-1"]
}
`

const testAccInstallationResourceValidationConfig_defaultAllInvalidRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org            = "test-org"
  client_id             = "test-client-id"
  selected_repositories = ["repo-1"]
}
`

const testAccInstallationResourceValidationConfig_validAll = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org           = "test-org"
  client_id            = "test-client-id"
  repository_selection = "all"
}
`

const testAccInstallationResourceValidationConfig_validSelected = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org            = "test-org"
  client_id             = "test-client-id"
  repository_selection  = "selected"
  selected_repositories = ["repo-1"]
}
`
