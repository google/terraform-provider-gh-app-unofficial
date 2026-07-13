package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestFlattenInstallations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockHTTPClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			respBody := `[
				{"id": 1, "name": "repo-alpha"},
				{"id": 2, "name": "repo-beta"}
			]`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(respBody)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	mockGHClient, _ := github.NewClient(github.WithHTTPClient(mockHTTPClient))

	permsMust := func(m map[string]attr.Value) types.Map {
		return types.MapValueMust(types.StringType, m)
	}
	eventsMust := func(s []string) types.List {
		var vals []attr.Value
		for _, v := range s {
			vals = append(vals, types.StringValue(v))
		}
		return types.ListValueMust(types.StringType, vals)
	}

	cases := []struct {
		name    string
		input   []*github.Installation
		rt      roundTripperFunc
		want    []app
		wantErr string
	}{
		{
			name:    "empty_list",
			input:   []*github.Installation{},
			want:    []app{},
			wantErr: "",
		},
		{
			name: "all_repositories_selection",
			input: []*github.Installation{
				{
					ID:                  github.Ptr(int64(11111)),
					ClientID:            github.Ptr("mock-client-id-abc"),
					AppSlug:             github.Ptr("test-app"),
					RepositorySelection: github.Ptr("all"),
					Permissions: &github.InstallationPermissions{
						Actions: github.Ptr("write"),
					},
					Events:    []string{"push", "pull_request"},
					CreatedAt: &github.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
					UpdatedAt: &github.Timestamp{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				},
			},
			want: []app{
				{
					ID:                   types.StringValue("11111"),
					ClientID:             types.StringValue("mock-client-id-abc"),
					AppSlug:              types.StringValue("test-app"),
					SelectedRepositories: types.ListNull(types.StringType),
					RepositorySelection:  types.StringValue("all"),
					Permissions:          permsMust(map[string]attr.Value{"actions": types.StringValue("write")}),
					Events:               eventsMust([]string{"push", "pull_request"}),
					CreatedAt:            types.StringValue("2026-01-01T00:00:00Z"),
					UpdatedAt:            types.StringValue("2026-01-02T00:00:00Z"),
				},
			},
			wantErr: "",
		},
		{
			name: "selected_repositories_selection",
			input: []*github.Installation{
				{
					ID:                  github.Ptr(int64(22222)),
					ClientID:            github.Ptr("mock-client-id-xyz"),
					AppSlug:             github.Ptr("selected-app"),
					RepositorySelection: github.Ptr("selected"),
					CreatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
					UpdatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				},
			},
			want: []app{
				{
					ID:                   types.StringValue("22222"),
					ClientID:             types.StringValue("mock-client-id-xyz"),
					AppSlug:              types.StringValue("selected-app"),
					SelectedRepositories: eventsMust([]string{"repo-alpha", "repo-beta"}),
					RepositorySelection:  types.StringValue("selected"),
					Permissions:          types.MapNull(types.StringType),
					Events:               types.ListNull(types.StringType),
					CreatedAt:            types.StringValue("2026-01-01T00:00:00Z"),
					UpdatedAt:            types.StringValue("2026-01-02T00:00:00Z"),
				},
			},
			wantErr: "",
		},
		{
			name: "selected_repositories_api_error",
			input: []*github.Installation{
				{
					ID:                  github.Ptr(int64(33333)),
					ClientID:            github.Ptr("mock-client-id-err"),
					AppSlug:             github.Ptr("err-app"),
					RepositorySelection: github.Ptr("selected"),
					CreatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
					UpdatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				},
			},
			rt: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("list repos failure")
			},
			want:    nil,
			wantErr: "Could not list repositories",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var diags diag.Diagnostics
			client := mockGHClient
			if tc.rt != nil {
				httpClient := &http.Client{Transport: tc.rt}
				client, _ = github.NewClient(github.WithHTTPClient(httpClient))
			}

			got := flattenInstallations(ctx, client, "test-ent", "test-org", tc.input, &diags)

			checkDiagnostics(t, diags, tc.wantErr)
			if tc.wantErr == "" {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("flattenInstallations() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestInstallationsDataSource_Unit_Configure(t *testing.T) {
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
			d := &installationsDataSource{}
			resp := &datasource.ConfigureResponse{}

			d.Configure(ctx, datasource.ConfigureRequest{ProviderData: tc.providerData}, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
			if d.client != tc.wantClient {
				t.Errorf("Configure() client = %v, want %v", d.client, tc.wantClient)
			}
		})
	}
}

// TestInstallationsDataSource_Unit_Metadata verifies that Metadata() sets the data source type name.
func TestInstallationsDataSource_Unit_Metadata(t *testing.T) {
	t.Parallel()
	d := &installationsDataSource{}
	var resp datasource.MetadataResponse
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "ghapp"}, &resp)
	if got, want := resp.TypeName, "ghapp_installations"; got != want {
		t.Errorf("Metadata() TypeName = %q, want %q", got, want)
	}
}

func TestInstallationsDataSource_Unit_Read(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rt      roundTripperFunc
		want    installation
		wantErr string
	}{
		{
			name: "read_success_200",
			rt: func(req *http.Request) (*http.Response, error) {
				body := `[{"id": 12345, "app_slug": "test-app", "repository_selection": "all", "created_at": "2026-07-01T20:00:00Z", "updated_at": "2026-07-01T20:00:00Z"}]`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Header:     make(http.Header),
				}, nil
			},
			want: installation{
				TargetOrg: types.StringValue("test-org"),
				Installations: []app{
					{
						ID:                   types.StringValue("12345"),
						ClientID:             types.StringValue(""),
						AppSlug:              types.StringValue("test-app"),
						SelectedRepositories: types.ListNull(types.StringType),
						RepositorySelection:  types.StringValue("all"),
						Permissions:          types.MapNull(types.StringType),
						Events:               types.ListNull(types.StringType),
						CreatedAt:            types.StringValue("2026-07-01T20:00:00Z"),
						UpdatedAt:            types.StringValue("2026-07-01T20:00:00Z"),
					},
				},
			},
			wantErr: "",
		},
		{
			name: "read_error_500",
			rt: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("list failure")
			},
			wantErr: "Failed to list installations",
		},
		{
			name: "read_flatten_installations_error",
			rt: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "repositories") {
					return nil, errors.New("list repos failure during read")
				}
				body := `[{"id": 12345, "app_slug": "test-app", "repository_selection": "selected", "created_at": "2026-07-01T20:00:00Z", "updated_at": "2026-07-01T20:00:00Z"}]`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Header:     make(http.Header),
				}, nil
			},
			wantErr: "Could not list repositories",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := &installationsDataSource{client: newTestGHClient(tc.rt)}

			var schemaResp datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)

			cfg := newTestConfig(t, ctx, schemaResp.Schema, installation{TargetOrg: types.StringValue("test-org")})
			resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}

			d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)

			checkDiagnostics(t, resp.Diagnostics, tc.wantErr)
			if tc.wantErr == "" {
				checkState(t, ctx, resp.State, tc.want)
			}
		})
	}
}
