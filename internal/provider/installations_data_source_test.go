package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
