package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// roundTripperFunc is a helper to mock HTTP responses in tests.
type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFlattenInstallations(t *testing.T) {
	// This is an offline unit test verifying that flattenInstallations
	// correctly maps go-github structs to Terraform state.
	// All inputs are synthetic mock values
	ctx := context.Background()

	// Mock HTTP client that returns a list of repositories when requested.
	mockHTTPClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Mock response for listing repositories for a selected installation.
			// This simulates the secondary HTTP request made by flattenInstallations
			// when an installation's repository selection is set to "selected".
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
	mockGHClient, err := github.NewClient(github.WithHTTPClient(mockHTTPClient))
	if err != nil {
		t.Fatalf("failed to create github client: %v", err)
	}

	cases := map[string]struct {
		input          []*github.Installation
		expectedLength int
		verify         func(t *testing.T, result []app)
	}{
		"Empty List": {
			input:          []*github.Installation{},
			expectedLength: 0,
			verify:         func(t *testing.T, result []app) {},
		},
		"All Repositories Selection": {
			// Validates mapping when the app is installed for ALL repositories in the org.
			// In this case, no additional API call is made to list repositories.
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
			expectedLength: 1,
			verify: func(t *testing.T, result []app) {
				res := result[0]
				if res.ID.ValueString() != "11111" {
					t.Errorf("expected ID 11111, got %s", res.ID.ValueString())
				}
				if res.ClientID.ValueString() != "mock-client-id-abc" {
					t.Errorf("expected ClientID mock-client-id-abc, got %s", res.ClientID.ValueString())
				}
				if res.AppSlug.ValueString() != "test-app" {
					t.Errorf("expected AppSlug test-app, got %s", res.AppSlug.ValueString())
				}
				if res.RepositorySelection.ValueString() != "all" {
					t.Errorf("expected RepositorySelection all, got %s", res.RepositorySelection.ValueString())
				}
				// Verify that since it's 'all' repositories, selected_repositories is null
				if !res.SelectedRepositories.IsNull() {
					t.Errorf("expected SelectedRepositories to be Null, got %v", res.SelectedRepositories)
				}

				// Verify permissions map
				var perms map[string]string
				diags := res.Permissions.ElementsAs(ctx, &perms, false) // To convert types.Map to map[string]string
				if diags.HasError() {
					t.Fatalf("failed to parse permissions: %v", diags.Errors())
				}
				if perms["actions"] != "write" {
					t.Errorf("expected actions permission 'write', got %s", perms["actions"])
				}
				if len(perms) != 1 {
					t.Errorf("expected 1 permission, got %d", len(perms))
				}

				// Verify events list
				var events []string
				diags = res.Events.ElementsAs(ctx, &events, false) // Converts from types.List to []string
				if diags.HasError() {
					t.Fatalf("failed to parse events: %v", diags.Errors())
				}
				if len(events) != 2 || events[0] != "push" || events[1] != "pull_request" {
					t.Errorf("unexpected events: %v", events)
				}
			},
		},
		"Selected Repositories Selection (Calls API)": {
			// Validates mapping when the app is installed for SELECTED repositories only.
			// This triggers the internal API call to retrieve the repository names list.
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
			expectedLength: 1,
			verify: func(t *testing.T, result []app) {
				res := result[0]
				if res.ID.ValueString() != "22222" {
					t.Errorf("expected ID 22222, got %s", res.ID.ValueString())
				}
				if res.RepositorySelection.ValueString() != "selected" {
					t.Errorf("expected RepositorySelection selected, got %s", res.RepositorySelection.ValueString())
				}
				// Verify that repositories were dynamically listed and flattened
				var repos []string
				diags := res.SelectedRepositories.ElementsAs(ctx, &repos, false)
				if diags.HasError() {
					t.Fatalf("failed to parse selected repositories: %v", diags.Errors())
				}
				if len(repos) != 2 || repos[0] != "repo-alpha" || repos[1] != "repo-beta" {
					t.Errorf("expected [repo-alpha, repo-beta], got %v", repos)
				}
			},
		},
		"Mixed Selection Modes (All and Selected)": {
			// Validates mapping when multiple installations are returned in a single organization,
			// mixing both 'all' repository selection and 'selected' repository selection.
			input: []*github.Installation{
				{
					ID:                  github.Ptr(int64(33333)),
					ClientID:            github.Ptr("mock-client-id-all"),
					AppSlug:             github.Ptr("app-all"),
					RepositorySelection: github.Ptr("all"),
					CreatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
					UpdatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				},
				{
					ID:                  github.Ptr(int64(44444)),
					ClientID:            github.Ptr("mock-client-id-selected"),
					AppSlug:             github.Ptr("app-selected"),
					RepositorySelection: github.Ptr("selected"),
					CreatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
					UpdatedAt:           &github.Timestamp{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				},
			},
			expectedLength: 2,
			verify: func(t *testing.T, result []app) {
				// Verify first installation ("all")
				resAll := result[0]
				if resAll.ID.ValueString() != "33333" {
					t.Errorf("expected first app ID 33333, got %s", resAll.ID.ValueString())
				}
				if resAll.RepositorySelection.ValueString() != "all" {
					t.Errorf("expected first app RepositorySelection all, got %s", resAll.RepositorySelection.ValueString())
				}
				if !resAll.SelectedRepositories.IsNull() {
					t.Errorf("expected first app SelectedRepositories to be Null, got %v", resAll.SelectedRepositories)
				}

				// Verify second installation ("selected")
				resSelected := result[1]
				if resSelected.ID.ValueString() != "44444" {
					t.Errorf("expected second app ID 44444, got %s", resSelected.ID.ValueString())
				}
				if resSelected.RepositorySelection.ValueString() != "selected" {
					t.Errorf("expected second app RepositorySelection selected, got %s", resSelected.RepositorySelection.ValueString())
				}
				var repos []string
				diags := resSelected.SelectedRepositories.ElementsAs(ctx, &repos, false)
				if diags.HasError() {
					t.Fatalf("failed to parse selected repositories for second app: %v", diags.Errors())
				}
				if len(repos) != 2 || repos[0] != "repo-alpha" || repos[1] != "repo-beta" {
					t.Errorf("expected [repo-alpha, repo-beta], got %v", repos)
				}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			result := flattenInstallations(ctx, mockGHClient, "test-ent", "test-org", tc.input, &diags)

			if diags.HasError() {
				t.Fatalf("flattenInstallations returned errors: %v", diags.Errors())
			}

			if len(result) != tc.expectedLength {
				t.Fatalf("expected result length %d, got %d", tc.expectedLength, len(result))
			}

			tc.verify(t, result)
		})
	}
}

func TestAccInstallationsDataSource(t *testing.T) {
	enterpriseSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccInstallationsDataSourceConfig(enterpriseSlug, targetOrg),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.ghapp_installations.test", "installations.#"),
				),
			},
		},
	})
}

func testAccInstallationsDataSourceConfig(enterpriseSlug, targetOrg string) string {
	return testAccProviderConfig(enterpriseSlug) + fmt.Sprintf(`
data "ghapp_installations" "test" {
	target_org = %[1]q
}
`, targetOrg)
}
