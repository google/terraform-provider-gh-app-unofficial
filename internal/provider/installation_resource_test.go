// Copyright 2026 Google LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// init registers a Terraform test sweeper for "gh-app-unofficial_installation" resources.
// Sweepers clean up dangling or orphaned live resources left behind if acceptance tests fail or crash before destroying resources.
// Sweepers can be executed on demand using `go test -v -sweep=all ./internal/provider/...`.
func init() {
	resource.AddTestSweepers("gh-app-unofficial_installation", &resource.Sweeper{
		Name: "gh-app-unofficial_installation",
		F: func(region string) error {
			ctx := context.Background()
			return sweepInstallations(ctx)
		},
	})
}

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func sweepInstallations(ctx context.Context) error {
	token := os.Getenv("GITHUB_TOKEN")
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	if token == "" || entSlug == "" || targetOrg == "" || clientID == "" {
		return nil
	}

	client, err := github.NewClient(github.WithAuthToken(token))
	if err != nil {
		return fmt.Errorf("failed to create client in sweeper: %w", err)
	}

	installations, err := listAllAppInstallations(ctx, client, entSlug, targetOrg)
	if err != nil {
		return fmt.Errorf("failed to list organization installations in sweeper: %w", err)
	}

	for _, inst := range installations {
		if inst.GetClientID() == clientID && inst.ID != nil {
			_, err := client.Enterprise.UninstallApp(ctx, entSlug, targetOrg, inst.GetID())
			if err != nil {
				return fmt.Errorf("failed to sweep app installation %d: %w", inst.GetID(), err)
			}
		}
	}

	return nil
}

func TestAccInstallationResource(t *testing.T) {
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstallationDestroy,
		Steps: []resource.TestStep{
			// Step 1: Create with selected repositories ("test-repo-1")
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "target_org", targetOrg),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "client_id", clientID),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.0", "test-repo-1"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "id"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "installation_id"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "created_at"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "updated_at"),
				),
			},
			// Step 2: Idempotency Verification on Create
			{
				Config:   testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				PlanOnly: true,
			},
			// Step 3: Import State Verification
			{
				ResourceName:            "gh-app-unofficial_installation.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"updated_at"},
			},
			// Step 4: Update - Repository Swap (to "test-repo-2")
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-2"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.0", "test-repo-2"),
				),
			},
			// Step 5: Idempotency Verification on Swap
			{
				Config:   testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-2"`),
				PlanOnly: true,
			},
			// Step 6: Update - Multi-Repository Expansion ("test-repo-1", "test-repo-2")
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1", "test-repo-2"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "2"),
				),
			},
			// Step 7: Update - Toggle Selection Mode to "all"
			{
				Config: testAccInstallationConfig_all(entSlug, targetOrg, clientID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "all"),
					resource.TestCheckNoResourceAttr("gh-app-unofficial_installation.test", "selected_repositories"),
				),
			},
			// Step 8: Idempotency Verification on Mode Toggle
			{
				Config:   testAccInstallationConfig_all(entSlug, targetOrg, clientID),
				PlanOnly: true,
			},
			// Step 9: Destroy & Re-Install Verification
			{
				Config:  testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Destroy: true,
			},
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.0", "test-repo-1"),
				),
			},
		},
	})
}

func TestAccInstallationResource_Drift(t *testing.T) {
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstallationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
				),
			},
			{
				// Out-of-band modification simulating external drift or refresh behavior
				PreConfig: func() {
					ctx := context.Background()
					token := os.Getenv("GITHUB_TOKEN")
					client, err := github.NewClient(github.WithAuthToken(token))
					if err != nil {
						t.Fatalf("PreConfig NewClient error: %v", err)
					}
					insts, _, err := client.Enterprise.ListAppInstallations(ctx, entSlug, targetOrg, nil)
					if err != nil {
						t.Fatalf("PreConfig ListAppInstallations error: %v", err)
					}
					found := false
					for _, inst := range insts {
						if inst.ClientID != nil && *inst.ClientID == clientID && inst.ID != nil {
							found = true
							opts := github.UpdateAppInstallationRepositoriesRequest{
								RepositorySelection: github.Ptr("selected"),
								Repositories:        []string{"test-repo-1", "test-repo-2"},
							}
							_, _, err := client.Enterprise.UpdateAppInstallationRepositories(ctx, entSlug, targetOrg, *inst.ID, opts)
							if err != nil {
								t.Fatalf("PreConfig UpdateAppInstallationRepositories error on inst %d: %v", *inst.ID, err)
							}
							t.Logf("PreConfig successfully updated inst %d to repositories %v", *inst.ID, opts.Repositories)
						}
					}
					if !found {
						t.Fatalf("PreConfig error: installation with client_id %s not found", clientID)
					}
				},
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "2"),
				),
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccInstallationResource_Unhappy(t *testing.T) {
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstallationConfig_selected(entSlug, "non-existent-sandbox-org-99999", "invalid-client-id-abc", `"test-repo-1"`),
				ExpectError: regexp.MustCompile("Failed to install app|installation not found|error"),
			},
		},
	})
}

func testAccCheckInstallationDestroy(s *terraform.State) error {
	ctx := context.Background()
	token := os.Getenv("GITHUB_TOKEN")
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	if token == "" || targetOrg == "" || clientID == "" {
		return nil
	}

	client, err := github.NewClient(github.WithAuthToken(token))
	if err != nil {
		return fmt.Errorf("error creating github client on CheckDestroy: %w", err)
	}
	insts, _, err := client.Enterprise.ListAppInstallations(ctx, entSlug, targetOrg, nil)
	if err != nil {
		return fmt.Errorf("error listing installations on CheckDestroy: %w", err)
	}

	for _, inst := range insts {
		if inst.ClientID != nil && *inst.ClientID == clientID {
			return fmt.Errorf("installation with client_id %s still exists after destroy", clientID)
		}
	}

	return nil
}

func testAccInstallationConfig_selected(enterpriseSlug, targetOrg, clientID, repos string) string {
	return fmt.Sprintf(`
provider "gh-app-unofficial" {
  enterprise_slug = %[1]q
}

resource "gh-app-unofficial_installation" "test" {
  target_org            = %[2]q
  client_id             = %[3]q
  repository_selection  = "selected"
  selected_repositories = [%[4]s]
}
`, enterpriseSlug, targetOrg, clientID, repos)
}

func testAccInstallationConfig_all(enterpriseSlug, targetOrg, clientID string) string {
	return fmt.Sprintf(`
provider "gh-app-unofficial" {
  enterprise_slug = %[1]q
}

resource "gh-app-unofficial_installation" "test" {
  target_org           = %[2]q
  client_id            = %[3]q
  repository_selection = "all"
}
`, enterpriseSlug, targetOrg, clientID)
}

func TestInstallationResource_ValidateConfig_Unit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		selection types.String
		repos     types.Set
		wantErr   string
	}{
		{
			name:      "all_selection_null_repos",
			selection: types.StringValue("all"),
			repos:     types.SetNull(types.StringType),
			wantErr:   "",
		},
		{
			name:      "all_selection_empty_repos",
			selection: types.StringValue("all"),
			repos:     types.SetValueMust(types.StringType, []attr.Value{}),
			wantErr:   "",
		},
		{
			name:      "all_selection_with_repos",
			selection: types.StringValue("all"),
			repos:     types.SetValueMust(types.StringType, []attr.Value{types.StringValue("repo1")}),
			wantErr:   "The selected_repositories attribute must not be set when repository_selection is 'all'.",
		},
		{
			name:      "selected_selection_valid_repos",
			selection: types.StringValue("selected"),
			repos:     types.SetValueMust(types.StringType, []attr.Value{types.StringValue("repo1")}),
			wantErr:   "",
		},
		{
			name:      "selected_selection_missing_repos",
			selection: types.StringValue("selected"),
			repos:     types.SetNull(types.StringType),
			wantErr:   "The selected_repositories attribute must be set when repository_selection is 'selected'.",
		},
		{
			name:      "selected_selection_empty_repos",
			selection: types.StringValue("selected"),
			repos:     types.SetValueMust(types.StringType, []attr.Value{}),
			wantErr:   "The selected_repositories attribute must contain at least one repository when repository_selection is 'selected'.",
		},
		{
			name:      "unknown_selection",
			selection: types.StringUnknown(),
			repos:     types.SetNull(types.StringType),
			wantErr:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{}

			model := installationResourceModel{
				RepositorySelection:  tc.selection,
				SelectedRepositories: tc.repos,
				Events:               types.ListNull(types.StringType),
				Permissions:          types.MapNull(types.StringType),
			}

			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

			req := fwresource.ValidateConfigRequest{
				Config: newTestConfig(t, ctx, schemaResp.Schema, model),
			}
			resp := &fwresource.ValidateConfigResponse{}

			r.ValidateConfig(ctx, req, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
		})
	}
}

func TestInstallationResource_Unit_Configure(t *testing.T) {
	t.Parallel()

	dummyClient := &GHClient{EnterpriseSlug: "ent"}

	cases := []struct {
		name         string
		providerData any
		wantClient   *GHClient
		wantErr      string
	}{
		{
			name:         "nil_provider_data",
			providerData: nil,
			wantClient:   nil,
			wantErr:      "",
		},
		{
			name:         "valid_provider_data",
			providerData: dummyClient,
			wantClient:   dummyClient,
			wantErr:      "",
		},
		{
			name:         "invalid_type",
			providerData: "not a gh client",
			wantClient:   nil,
			wantErr:      "Unexpected Configure Type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{}
			resp := &fwresource.ConfigureResponse{}

			r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: tc.providerData}, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
			if r.client != tc.wantClient {
				t.Errorf("Configure() client = %v, want %v", r.client, tc.wantClient)
			}
		})
	}
}

func TestInstallationResource_Unit_Metadata(t *testing.T) {
	t.Parallel()
	r := &installationResource{}
	var resp fwresource.MetadataResponse
	r.Metadata(context.Background(), fwresource.MetadataRequest{ProviderTypeName: "ghapp"}, &resp)
	if got, want := resp.TypeName, "ghapp_installation"; got != want {
		t.Errorf("Metadata() TypeName = %q, want %q", got, want)
	}
}

func TestInstallationResource_Unit_Create(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rt      roundTripperFunc
		wantErr string
	}{
		{
			name:    "create_success_200",
			rt:      mockHTTPResponse(http.StatusOK, `{"id": 12345, "app_slug": "test-app", "repository_selection": "all", "created_at": "2026-07-01T20:00:00Z", "updated_at": "2026-07-01T20:00:00Z"}`),
			wantErr: "",
		},
		{
			name:    "create_error_500",
			rt:      mockHTTPError("API error"),
			wantErr: "Failed to install app",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{client: newTestGHClient(tc.rt)}

			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

			model := newTestResourceModel("54321", "new-test-org", "new-client-abc", "all", nil)
			req := fwresource.CreateRequest{Plan: newTestPlan(t, ctx, schemaResp.Schema, model)}
			resp := &fwresource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}

			r.Create(ctx, req, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
		})
	}
}

func TestInstallationResource_Unit_Read(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rt      roundTripperFunc
		wantErr string
	}{
		{
			name:    "read_success_200",
			rt:      mockHTTPResponse(http.StatusOK, `[{"id": 12345, "app_slug": "test-app", "repository_selection": "all", "created_at": "2026-07-01T20:00:00Z", "updated_at": "2026-07-01T20:00:00Z"}]`),
			wantErr: "",
		},
		{
			name:    "read_not_found_removes_resource",
			rt:      mockHTTPResponse(http.StatusOK, `[]`),
			wantErr: "",
		},
		{
			name:    "read_error_500",
			rt:      mockHTTPError("network failure"),
			wantErr: "Error Reading GitHub App Installations",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{client: newTestGHClient(tc.rt)}

			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

			model := newTestResourceModel("test-org/12345", "test-org", "client-abc", "all", nil)
			state := newTestState(t, ctx, schemaResp.Schema, model)
			req := fwresource.ReadRequest{State: state}
			resp := &fwresource.ReadResponse{State: state}

			r.Read(ctx, req, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
		})
	}
}

func TestInstallationResource_Unit_Update(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		id      string
		rt      roundTripperFunc
		wantErr string
	}{
		{
			name:    "update_success_200",
			rt:      mockHTTPResponse(http.StatusOK, `{"id": 12345, "app_slug": "test-app", "repository_selection": "all", "created_at": "2026-07-01T20:00:00Z", "updated_at": "2026-07-01T20:00:00Z"}`),
			wantErr: "",
		},
		{
			name:    "update_error_500",
			rt:      mockHTTPError("update error"),
			wantErr: "Failed to update app installation repositories",
		},
		{
			name:    "update_nil_installation",
			rt:      mockHTTPResponse(http.StatusOK, "null"),
			wantErr: "Installation not found",
		},
		{
			name:    "update_invalid_id",
			id:      "not-an-integer",
			rt:      nil,
			wantErr: "Unexpected Identifier",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{client: newTestGHClient(tc.rt)}

			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

			id := tc.id
			if id == "" {
				id = "test-org/12345"
			}

			model := newTestResourceModel(id, "test-org", "client-abc", "selected", []string{"test-repo-1"})
			plan := newTestPlan(t, ctx, schemaResp.Schema, model)
			req := fwresource.UpdateRequest{Plan: plan}
			resp := &fwresource.UpdateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}

			r.Update(ctx, req, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
		})
	}
}

func TestInstallationResource_Unit_Delete(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		id      string
		rt      roundTripperFunc
		wantErr string
	}{
		{
			name:    "delete_success_204",
			rt:      mockHTTPResponse(http.StatusNoContent, ""),
			wantErr: "",
		},
		{
			name:    "delete_error_500",
			rt:      mockHTTPError("uninstall failure"),
			wantErr: "Failed to uninstall app",
		},
		{
			name:    "delete_invalid_id",
			id:      "not-an-integer",
			rt:      nil,
			wantErr: "Unexpected Identifier",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{client: newTestGHClient(tc.rt)}

			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

			id := tc.id
			if id == "" {
				id = "test-org/12345"
			}

			model := newTestResourceModel(id, "test-org", "client-abc", "all", nil)
			state := newTestState(t, ctx, schemaResp.Schema, model)
			req := fwresource.DeleteRequest{State: state}
			resp := &fwresource.DeleteResponse{State: state}

			r.Delete(ctx, req, resp)

			checkErrorOrDiags(t, resp.Diagnostics, tc.wantErr)
		})
	}
}

func TestInstallationResource_Unit_Import(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		id          string
		wantReadErr string
	}{
		{
			name:        "import_success",
			id:          "test-org/12345",
			wantReadErr: "",
		},
		{
			name:        "import_invalid_id",
			id:          "not-an-integer",
			wantReadErr: "Unexpected Identifier",
		},
		{
			name:        "import_invalid_format",
			id:          "test-org/not-an-integer",
			wantReadErr: "Unexpected Identifier",
		},
		{
			name:        "import_invalid_flipped",
			id:          "12456/test-org",
			wantReadErr: "Unexpected Identifier",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			rt := mockHTTPResponse(http.StatusOK, `[{"id": 12345, "app_slug": "test-app", "repository_selection": "all", "created_at": "2026-07-01T20:00:00Z", "updated_at": "2026-07-01T20:00:00Z"}]`)
			r := &installationResource{client: newTestGHClient(rt)}

			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

			model := newTestResourceModel("", "", "", "", nil)
			state := newTestState(t, ctx, schemaResp.Schema, model)

			req := fwresource.ImportStateRequest{
				ID: tc.id,
			}
			resp := &fwresource.ImportStateResponse{State: state}

			r.ImportState(ctx, req, resp)
			checkErrorOrDiags(t, resp.Diagnostics, "")

			readReq := fwresource.ReadRequest{State: resp.State}
			readResp := &fwresource.ReadResponse{State: resp.State}
			r.Read(ctx, readReq, readResp)

			checkErrorOrDiags(t, readResp.Diagnostics, tc.wantReadErr)
		})
	}
}
