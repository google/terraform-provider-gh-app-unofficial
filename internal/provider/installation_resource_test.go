package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestInstallationResource_ValidateConfig_Unit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		selection types.String
		repos     types.List
		wantErr   string
	}{
		{
			name:      "all_selection_null_repos",
			selection: types.StringValue("all"),
			repos:     types.ListNull(types.StringType),
			wantErr:   "",
		},
		{
			name:      "all_selection_empty_repos",
			selection: types.StringValue("all"),
			repos:     types.ListValueMust(types.StringType, []attr.Value{}),
			wantErr:   "",
		},
		{
			name:      "all_selection_with_repos",
			selection: types.StringValue("all"),
			repos:     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("repo1")}),
			wantErr:   "The selected_repositories attribute must not be set when repository_selection is 'all'.",
		},
		{
			name:      "selected_selection_valid_repos",
			selection: types.StringValue("selected"),
			repos:     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("repo1")}),
			wantErr:   "",
		},
		{
			name:      "selected_selection_missing_repos",
			selection: types.StringValue("selected"),
			repos:     types.ListNull(types.StringType),
			wantErr:   "The selected_repositories attribute must be set when repository_selection is 'selected'.",
		},
		{
			name:      "selected_selection_empty_repos",
			selection: types.StringValue("selected"),
			repos:     types.ListValueMust(types.StringType, []attr.Value{}),
			wantErr:   "The selected_repositories attribute must contain at least one repository when repository_selection is 'selected'.",
		},
		{
			name:      "unknown_repo_selection",
			selection: types.StringUnknown(),
			repos:     types.ListNull(types.StringType),
			wantErr:   "",
		},
		{
			name:      "null_repo_selection_with_null_repos",
			selection: types.StringNull(),
			repos:     types.ListNull(types.StringType),
			wantErr:   "",
		},
		{
			name:      "null_repo_selection_with_repos",
			selection: types.StringNull(),
			repos:     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("repo1")}),
			wantErr:   "The selected_repositories attribute must not be set when repository_selection is 'all'.",
		},
		{
			name:      "selected_selection_unknown_repos",
			selection: types.StringValue("selected"),
			repos:     types.ListUnknown(types.StringType),
			wantErr:   "",
		},
		{
			name:      "all_selection_unknown_repos",
			selection: types.StringValue("all"),
			repos:     types.ListUnknown(types.StringType),
			wantErr:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := &installationResource{}

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			model := installationResourceModel{
				TargetOrg:            types.StringValue("test-org"),
				ClientID:             types.StringValue("test-client-id"),
				SelectedRepositories: tc.repos,
				RepositorySelection:  tc.selection,
				Events:               types.ListNull(types.StringType),
				Permissions:          types.MapNull(types.StringType),
			}

			req := resource.ValidateConfigRequest{
				Config: newTestConfig(t, ctx, schemaResp.Schema, model),
			}
			resp := &resource.ValidateConfigResponse{}

			r.ValidateConfig(ctx, req, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
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
			name:         "invalid_provider_data_type",
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
			resp := &resource.ConfigureResponse{}

			r.Configure(ctx, resource.ConfigureRequest{ProviderData: tc.providerData}, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
			if r.client != tc.wantClient {
				t.Errorf("Configure() client = %v, want %v", r.client, tc.wantClient)
			}
		})
	}
}

// TestInstallationResource_Unit_Metadata verifies that Metadata() sets the resource type name.
func TestInstallationResource_Unit_Metadata(t *testing.T) {
	t.Parallel()
	r := &installationResource{}
	var resp resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "ghapp"}, &resp)
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

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			model := newTestResourceModel("54321", "new-test-org", "new-client-abc", "all", nil)
			req := resource.CreateRequest{Plan: newTestPlan(t, ctx, schemaResp.Schema, model)}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}

			r.Create(ctx, req, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
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

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			model := newTestResourceModel("test-org/12345", "test-org", "client-abc", "all", nil)
			state := newTestState(t, ctx, schemaResp.Schema, model)
			req := resource.ReadRequest{State: state}
			resp := &resource.ReadResponse{State: state}

			r.Read(ctx, req, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
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

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			id := tc.id
			if id == "" {
				id = "test-org/12345"
			}

			model := newTestResourceModel(id, "test-org", "client-abc", "selected", []string{"test-repo-1"})
			plan := newTestPlan(t, ctx, schemaResp.Schema, model)
			req := resource.UpdateRequest{Plan: plan}
			resp := &resource.UpdateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}

			r.Update(ctx, req, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
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

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			id := tc.id
			if id == "" {
				id = "test-org/12345"
			}

			model := newTestResourceModel(id, "test-org", "client-abc", "all", nil)
			state := newTestState(t, ctx, schemaResp.Schema, model)
			req := resource.DeleteRequest{State: state}
			resp := &resource.DeleteResponse{State: state}

			r.Delete(ctx, req, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
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

			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

			model := newTestResourceModel("", "", "", "", nil)
			state := newTestState(t, ctx, schemaResp.Schema, model)

			req := resource.ImportStateRequest{
				ID: tc.id,
			}
			resp := &resource.ImportStateResponse{State: state}

			r.ImportState(ctx, req, resp)
			checkDiagnostics(t, resp.Diagnostics, "")

			readReq := resource.ReadRequest{State: resp.State}
			readResp := &resource.ReadResponse{State: resp.State}
			r.Read(ctx, readReq, readResp)

			checkDiagnostics(t, readResp.Diagnostics, tc.wantReadErr)
		})
	}
}
