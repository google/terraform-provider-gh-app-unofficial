package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip executes a single HTTP transaction, invoking the underlying functional mock directly.
func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func diffErrString(errOrDiags any, wantErr string) string {
	if diags, ok := errOrDiags.(diag.Diagnostics); ok {
		if wantErr == "" {
			if diags.HasError() {
				return fmt.Sprintf("got unexpected diagnostics: %v", diags)
			}
			return ""
		}

		if !diags.HasError() {
			return fmt.Sprintf("expected error containing %q, got no error", wantErr)
		}
		for _, d := range diags.Errors() {
			if strings.Contains(d.Summary(), wantErr) || strings.Contains(d.Detail(), wantErr) {
				return ""
			}
		}
		return fmt.Sprintf("expected error containing %q, got diagnostics: %v", wantErr, diags)
	}

	if err, ok := errOrDiags.(error); ok {
		if wantErr == "" {
			if err != nil {
				return fmt.Sprintf("got unexpected error: %v", err)
			}
			return ""
		}
		if err == nil {
			return fmt.Sprintf("expected error containing %q, got nil", wantErr)
		}
		if !strings.Contains(err.Error(), wantErr) {
			return fmt.Sprintf("expected error containing %q, got: %v", wantErr, err)
		}
		return ""
	}

	if wantErr == "" && errOrDiags == nil {
		return ""
	}

	return fmt.Sprintf("unsupported type for diffErrString: %T", errOrDiags)
}

// checkDiagnostics asserts that diags (diag.Diagnostics) or err (error) contains (or doesn't contain) the expected error substring.
func checkDiagnostics(t *testing.T, errOrDiags any, wantErr string) {
	t.Helper()
	if diff := diffErrString(errOrDiags, wantErr); diff != "" {
		t.Error(diff)
	}
}

func TestFormatBaseURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rawURL  string
		want    string
		wantErr string
	}{
		{
			name:    "empty_url",
			rawURL:  "",
			wantErr: "base URL must not be empty",
		},
		{
			name:    "standard_domain_no_slash",
			rawURL:  "https://github.mycompany.com",
			want:    "https://github.mycompany.com/api/v3/",
			wantErr: "",
		},
		{
			name:    "standard_domain_with_slash",
			rawURL:  "https://github.mycompany.com/",
			want:    "https://github.mycompany.com/api/v3/",
			wantErr: "",
		},
		{
			name:    "domain_with_api_v3_no_slash",
			rawURL:  "https://github.mycompany.com/api/v3",
			want:    "https://github.mycompany.com/api/v3/",
			wantErr: "",
		},
		{
			name:    "domain_with_api_v3_with_slash",
			rawURL:  "https://github.mycompany.com/api/v3/",
			want:    "https://github.mycompany.com/api/v3/",
			wantErr: "",
		},
		{
			name:    "https_custom_port",
			rawURL:  "https://127.0.0.1:8080",
			want:    "https://127.0.0.1:8080/api/v3/",
			wantErr: "",
		},
		{
			name:    "http_scheme_disallowed",
			rawURL:  "http://127.0.0.1:8080",
			wantErr: "base URL must use the https scheme",
		},
		{
			name:    "subpath_domain",
			rawURL:  "https://corp.com/ghe",
			want:    "https://corp.com/ghe/",
			wantErr: "",
		},
		{
			name:    "depot_domain",
			rawURL:  "https://depot.code.corp.goog/",
			want:    "https://depot.code.corp.goog/api/v3/",
			wantErr: "",
		},
		{
			name:    "depot_domain_no_schema",
			rawURL:  "depot.code.corp.goog",
			want:    "https://depot.code.corp.goog/api/v3/",
			wantErr: "",
		},
		{
			name:    "dotcom_api",
			rawURL:  "https://api.github.com",
			want:    "https://api.github.com/",
			wantErr: "",
		},
		{
			name:    "dotcom_ui",
			rawURL:  "https://github.com",
			want:    "https://api.github.com/",
			wantErr: "",
		},
		{
			name:    "ghec_ui",
			rawURL:  "https://customer.ghe.com",
			want:    "https://customer.ghe.com/api/v3/",
			wantErr: "",
		},
		{
			name:    "invalid_url",
			rawURL:  "://invalid-url",
			wantErr: "missing protocol scheme",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var diags diag.Diagnostics
			got := formatBaseURL(tc.rawURL, &diags)
			checkDiagnostics(t, diags, tc.wantErr)
			if tc.wantErr == "" && got != tc.want {
				t.Errorf("formatBaseURL(%q) = %q, want %q", tc.rawURL, got, tc.want)
			}
		})
	}
}

func TestListToStringSlice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		list types.List
		want []string
	}{
		{
			name: "known_list_with_elements",
			list: types.ListValueMust(types.StringType, []attr.Value{
				types.StringValue("repo1"),
				types.StringValue("repo2"),
			}),
			want: []string{"repo1", "repo2"},
		},
		{
			name: "empty_known_list",
			list: types.ListValueMust(types.StringType, []attr.Value{}),
			want: []string{},
		},
		{
			name: "null_list",
			list: types.ListNull(types.StringType),
			want: nil,
		},
		{
			name: "unknown_list",
			list: types.ListUnknown(types.StringType),
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var diags diag.Diagnostics

			got := listToStringSlice(ctx, tc.list, &diags)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("listToStringSlice() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIsKnown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		val  attr.Value
		want bool
	}{
		{
			name: "known_string",
			val:  types.StringValue("hello"),
			want: true,
		},
		{
			name: "null_string",
			val:  types.StringNull(),
			want: false,
		},
		{
			name: "unknown_string",
			val:  types.StringUnknown(),
			want: false,
		},
		{
			name: "nil_value",
			val:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isKnown(tc.val)
			if got != tc.want {
				t.Errorf("isKnown() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFlattenPermissions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		permissions *github.InstallationPermissions
		wantErr     string
	}{
		{
			name: "valid_permissions",
			permissions: &github.InstallationPermissions{
				Issues: github.Ptr("write"),
			},
			wantErr: "",
		},
		{
			name:        "nil_permissions",
			permissions: nil,
			wantErr:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var diags diag.Diagnostics

			got := flattenPermissions(ctx, tc.permissions, &diags)

			checkDiagnostics(t, diags, tc.wantErr)

			if tc.wantErr == "" && tc.permissions != nil {
				if got.IsNull() || got.IsUnknown() {
					t.Errorf("expected known map, got %v", got)
				}
			}
		})
	}
}

func TestGetSelectedRepositories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		selection string
		rt        roundTripperFunc
		wantCount int
		wantErr   string
	}{
		{
			name:      "not_selected_returns_null",
			selection: "all",
			rt:        nil, // Transport is nil because repository_selection="all" short-circuits before making external API calls.
			wantCount: 0,
			wantErr:   "",
		},
		{
			name:      "selected_success_200",
			selection: "selected",
			rt: func(req *http.Request) (*http.Response, error) {
				body := `[{"name":"repo1"},{"name":"repo2"}]`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Header:     make(http.Header),
				}, nil
			},
			wantCount: 2,
			wantErr:   "",
		},
		{
			name:      "selected_error_500",
			selection: "selected",
			rt: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network timeout")
			},
			wantCount: 0,
			wantErr:   "Could not list repositories",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var diags diag.Diagnostics

			httpClient := &http.Client{Transport: tc.rt}
			client, _ := github.NewClient(github.WithHTTPClient(httpClient))

			got := getSelectedRepositories(ctx, client, "test-ent", "test-org", 123, tc.selection, &diags)

			checkDiagnostics(t, diags, tc.wantErr)

			if tc.wantErr == "" && tc.selection == "selected" {
				if got.IsNull() || got.IsUnknown() {
					t.Fatalf("expected known list, got %v", got)
				}
				if len(got.Elements()) != tc.wantCount {
					t.Errorf("expected %d elements, got %d", tc.wantCount, len(got.Elements()))
				}
			}
		})
	}
}

func TestGetGHClient(t *testing.T) {
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
			var diags diag.Diagnostics

			got := getGHClient(ctx, tc.providerData, &diags)

			checkDiagnostics(t, diags, tc.wantErr)
			if got != tc.wantClient {
				t.Errorf("getGHClient() = %v, want %v", got, tc.wantClient)
			}
		})
	}
}

func TestParseCompositeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		id            string
		wantTargetOrg string
		wantInstID    int64
		wantInstIDStr string
		wantErrSubstr string
	}{
		{
			name:          "valid_composite_id",
			id:            "test-org/12345678",
			wantTargetOrg: "test-org",
			wantInstID:    12345678,
			wantInstIDStr: "12345678",
			wantErrSubstr: "",
		},
		{
			name:          "valid_composite_id_with_special_chars_in_org",
			id:            "org_name-with.dots/999",
			wantTargetOrg: "org_name-with.dots",
			wantInstID:    999,
			wantInstIDStr: "999",
			wantErrSubstr: "",
		},
		{
			name:          "missing_slash",
			id:            "test-org-12345678",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "too_many_slashes",
			id:            "test-org/12345678/extra",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "empty_target_org",
			id:            "/12345678",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "empty_installation_id",
			id:            "test-org/",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "empty_id",
			id:            "",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "non_integer_installation_id",
			id:            "test-org/not-a-number",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "installation ID must be an integer",
		},
		{
			name:          "overflow_installation_id",
			id:            "test-org/999999999999999999999999999999999",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErrSubstr: "installation ID must be an integer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotOrg, gotInstID, gotInstIDStr, err := parseCompositeID(tc.id)

			checkDiagnostics(t, err, tc.wantErrSubstr)
			if tc.wantErrSubstr == "" {
				if gotOrg != tc.wantTargetOrg {
					t.Errorf("targetOrg = %q, want %q", gotOrg, tc.wantTargetOrg)
				}
				if gotInstID != tc.wantInstID {
					t.Errorf("instID = %d, want %d", gotInstID, tc.wantInstID)
				}
				if gotInstIDStr != tc.wantInstIDStr {
					t.Errorf("instIDStr = %q, want %q", gotInstIDStr, tc.wantInstIDStr)
				}
			}
		})
	}
}

