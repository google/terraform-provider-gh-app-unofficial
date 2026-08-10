// Copyright 2026 Google LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// checkErrorOrDiags asserts whether errOrDiags (which accepts diag.Diagnostics, error, or nil)
// matches the expected wantErr substring in its summary, details, or error message.
// If wantErr is empty, it asserts that no errors or error-level diagnostics occurred.
func checkErrorOrDiags(t *testing.T, errOrDiags any, wantErr string) {
	t.Helper()
	if diff := diffErrString(errOrDiags, wantErr); diff != "" {
		t.Fatal(diff)
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
			checkErrorOrDiags(t, diags, tc.wantErr)
			if tc.wantErr == "" && got != tc.want {
				t.Errorf("formatBaseURL(%q) = %q, want %q", tc.rawURL, got, tc.want)
			}
		})
	}
}

func TestSetToStringSlice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		set  types.Set
		want []string
	}{
		{
			name: "known_set_with_elements",
			set: types.SetValueMust(types.StringType, []attr.Value{
				types.StringValue("repo1"),
				types.StringValue("repo2"),
			}),
			want: []string{"repo1", "repo2"},
		},
		{
			name: "empty_known_set",
			set:  types.SetValueMust(types.StringType, []attr.Value{}),
			want: []string{},
		},
		{
			name: "null_set",
			set:  types.SetNull(types.StringType),
			want: nil,
		},
		{
			name: "unknown_set",
			set:  types.SetUnknown(types.StringType),
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var diags diag.Diagnostics

			got := setToStringSlice(ctx, tc.set, &diags)

			checkErrorOrDiags(t, diags, "")

			sort.Strings(got)
			sort.Strings(tc.want)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("setToStringSlice() mismatch (-want +got):\n%s", diff)
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

			checkErrorOrDiags(t, diags, tc.wantErr)
			if tc.wantErr != "" {
				return
			}

			if tc.permissions == nil {
				if !got.IsNull() {
					t.Errorf("expected null map for nil permissions, got %v", got)
				}
				return
			}

			if got.IsNull() || got.IsUnknown() {
				t.Errorf("expected known map, got %v", got)
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
		{
			name:      "selected_multi_page_success",
			selection: "selected",
			rt: func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				body := ""
				if req.URL.Query().Get("page") == "2" {
					body = `[{"name":"repo3"}]`
				} else {
					body = `[{"name":"repo1"},{"name":"repo2"}]`
					header.Set("Link", `<https://api.github.com/enterprises/test-ent/apps/organizations/test-org/installations/123/repositories?page=2>; rel="next"`)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Header:     header,
				}, nil
			},
			wantCount: 3,
			wantErr:   "",
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

			checkErrorOrDiags(t, diags, tc.wantErr)
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

			checkErrorOrDiags(t, diags, tc.wantErr)
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
		wantErr       string
	}{
		{
			name:          "valid_composite_id",
			id:            "test-org/12345678",
			wantTargetOrg: "test-org",
			wantInstID:    12345678,
			wantInstIDStr: "12345678",
			wantErr:       "",
		},
		{
			name:          "valid_composite_id_with_special_chars_in_org",
			id:            "org_name-with.dots/999",
			wantTargetOrg: "org_name-with.dots",
			wantInstID:    999,
			wantInstIDStr: "999",
			wantErr:       "",
		},
		{
			name:          "missing_slash",
			id:            "test-org-12345678",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "too_many_slashes",
			id:            "test-org/12345678/extra",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "empty_target_org",
			id:            "/12345678",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "empty_installation_id",
			id:            "test-org/",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "empty_id",
			id:            "",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "ID must be in the format <target_org>/<installation_id>",
		},
		{
			name:          "non_integer_installation_id",
			id:            "test-org/not-a-number",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "installation ID must be an integer",
		},
		{
			name:          "overflow_installation_id",
			id:            "test-org/999999999999999999999999999999999",
			wantTargetOrg: "",
			wantInstID:    0,
			wantInstIDStr: "",
			wantErr:       "installation ID must be an integer",
		},
		{
			name:          "negative_installation_id",
			id:            "test-org/-1",
			wantTargetOrg: "test-org",
			wantInstID:    -1,
			wantInstIDStr: "-1",
			wantErr:       "",
		},
		{
			name:          "zero_installation_id",
			id:            "test-org/0",
			wantTargetOrg: "test-org",
			wantInstID:    0,
			wantInstIDStr: "0",
			wantErr:       "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotOrg, gotInstID, gotInstIDStr, err := parseCompositeID(tc.id)

			checkErrorOrDiags(t, err, tc.wantErr)
			if tc.wantErr == "" {
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

func TestListAllAppInstallations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		rt        roundTripperFunc
		wantCount int
		wantErr   string
	}{
		{
			name: "single_page_installations",
			rt: func(req *http.Request) (*http.Response, error) {
				body := `[{"id":1,"app_slug":"app-1"},{"id":2,"app_slug":"app-2"}]`
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
			name: "multi_page_installations",
			rt: func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				body := ""
				if req.URL.Query().Get("page") == "2" {
					body = `[{"id":3,"app_slug":"app-3"}]`
				} else {
					body = `[{"id":1,"app_slug":"app-1"},{"id":2,"app_slug":"app-2"}]`
					header.Set("Link", `<https://api.github.com/enterprises/my-ent/apps/organizations/my-org/installations?page=2>; rel="next"`)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Header:     header,
				}, nil
			},
			wantCount: 3,
			wantErr:   "",
		},
		{
			name: "api_error_returns_error",
			rt: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network failure")
			},
			wantCount: 0,
			wantErr:   "network failure",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			httpClient := &http.Client{Transport: tc.rt}
			client, _ := github.NewClient(github.WithHTTPClient(httpClient))

			got, err := listAllAppInstallations(ctx, client, "my-ent", "my-org")

			checkErrorOrDiags(t, err, tc.wantErr)
			if tc.wantErr == "" && len(got) != tc.wantCount {
				t.Errorf("expected %d installations, got %d", tc.wantCount, len(got))
			}
		})
	}
}

func TestListAllRepositoriesForOrgAppInstallation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		rt        roundTripperFunc
		wantCount int
		wantErr   string
	}{
		{
			name: "single_page_repositories",
			rt: func(req *http.Request) (*http.Response, error) {
				body := `[{"name":"repo-1"},{"name":"repo-2"}]`
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
			name: "multi_page_repositories",
			rt: func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				body := ""
				if req.URL.Query().Get("page") == "2" {
					body = `[{"name":"repo-3"}]`
				} else {
					body = `[{"name":"repo-1"},{"name":"repo-2"}]`
					header.Set("Link", `<https://api.github.com/enterprises/my-ent/apps/organizations/my-org/installations/123/repositories?page=2>; rel="next"`)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Header:     header,
				}, nil
			},
			wantCount: 3,
			wantErr:   "",
		},
		{
			name: "api_error_returns_error",
			rt: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
			wantCount: 0,
			wantErr:   "network error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			httpClient := &http.Client{Transport: tc.rt}
			client, _ := github.NewClient(github.WithHTTPClient(httpClient))

			got, err := listAllRepositoriesForOrgAppInstallation(ctx, client, "my-ent", "my-org", 123)

			checkErrorOrDiags(t, err, tc.wantErr)
			if tc.wantErr == "" && len(got) != tc.wantCount {
				t.Errorf("expected %d repositories, got %d", tc.wantCount, len(got))
			}
		})
	}
}

func TestListAppInstallationsCached_Singleflight(t *testing.T) {
	t.Parallel()

	var reqCount atomic.Int32
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		reqCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		body := `[{"id":1,"app_slug":"app-1"}]`
		header := make(http.Header)
		header.Set("ETag", `"etag-123"`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Header:     header,
		}, nil
	})

	httpClient := &http.Client{Transport: rt}
	ghClient, _ := github.NewClient(github.WithHTTPClient(httpClient))
	client := &GHClient{
		EnterpriseSlug: "my-ent",
		Client:         ghClient,
	}

	ctx := context.Background()
	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			insts, err := client.ListAppInstallationsCached(ctx, "my-org")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(insts) != 1 {
				t.Errorf("expected 1 installation, got %d", len(insts))
			}
		}()
	}

	wg.Wait()

	if count := reqCount.Load(); count != 1 {
		t.Errorf("expected exactly 1 HTTP request due to singleflight coalescing, got %d", count)
	}
}

func TestETagTransport_ClonesRequest(t *testing.T) {
	t.Parallel()

	var receivedETag atomic.Pointer[string]
	mockRT := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		etag := req.Header.Get("If-None-Match")
		receivedETag.Store(&etag)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
			Header:     make(http.Header),
		}, nil
	})

	transport := &etagTransport{transport: mockRT}

	ctx := context.WithValue(context.Background(), ctxEtagKey, `"etag-xyz"`)
	origReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/enterprises/test/apps/organizations/test/installations", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if origHeader := origReq.Header.Get("If-None-Match"); origHeader != "" {
		t.Fatalf("expected initial If-None-Match to be empty, got %q", origHeader)
	}

	resp, err := transport.RoundTrip(origReq)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	gotETag := ""
	if p := receivedETag.Load(); p != nil {
		gotETag = *p
	}
	if gotETag != `"etag-xyz"` {
		t.Errorf("expected transported request to have If-None-Match %q, got %q", `"etag-xyz"`, gotETag)
	}

	// Verify the original request was NOT mutated (http.RoundTripper contract)
	if origHeader := origReq.Header.Get("If-None-Match"); origHeader != "" {
		t.Errorf("expected original request header to remain unmutated (empty), got %q", origHeader)
	}
}

func TestOrgInstallationCache_Operations(t *testing.T) {
	t.Parallel()

	cache := newOrgInstallationCache(100 * time.Millisecond)

	// 1. Initial lookup on empty cache
	entry, isFresh, exists := cache.Get("org-1")
	if exists || isFresh || len(entry.installations) != 0 || entry.etag != "" {
		t.Fatalf("expected empty cache miss, got exists=%v, isFresh=%v, len=%d, etag=%s", exists, isFresh, len(entry.installations), entry.etag)
	}

	// 2. Set entry
	dummyInsts := []*github.Installation{{ID: github.Ptr(int64(42))}}
	cache.Set("org-1", dummyInsts, `"etag-42"`)

	entry, isFresh, exists = cache.Get("org-1")
	if !exists || !isFresh || len(entry.installations) != 1 || entry.etag != `"etag-42"` {
		t.Fatalf("expected fresh cache hit, got exists=%v, isFresh=%v, len=%d, etag=%s", exists, isFresh, len(entry.installations), entry.etag)
	}

	// 3. Expire entry
	cache.Expire("org-1")
	entry, isFresh, exists = cache.Get("org-1")
	if !exists {
		t.Fatalf("expected expired entry to still exist for conditional GET")
	}
	if isFresh {
		t.Fatalf("expected expired entry to have isFresh=false")
	}
	if entry.etag != `"etag-42"` || len(entry.installations) != 1 {
		t.Fatalf("expected expired entry to preserve installations and etag")
	}

	// 4. Touch entry (HTTP 304 Not Modified refresh)
	cache.Touch("org-1")
	entry, isFresh, exists = cache.Get("org-1")
	if !exists || !isFresh || len(entry.installations) != 1 {
		t.Fatalf("expected touched entry to be fresh, got exists=%v, isFresh=%v", exists, isFresh)
	}

	// 5. Invalidate entry (Write operation)
	cache.Invalidate("org-1")
	_, isFresh, exists = cache.Get("org-1")
	if exists || isFresh {
		t.Fatalf("expected invalidated entry to not exist, got exists=%v, isFresh=%v", exists, isFresh)
	}

	// 6. Nil receiver safety
	var nilCache *orgInstallationCache
	_, nilFresh, nilExists := nilCache.Get("org-1")
	if nilExists || nilFresh {
		t.Fatalf("expected nil cache Get to return false")
	}
	nilCache.Set("org-1", dummyInsts, "etag")
	nilCache.Touch("org-1")
	nilCache.Invalidate("org-1")
	nilCache.Expire("org-1")
}

func TestListAppInstallationsCached_ETag304(t *testing.T) {
	t.Parallel()

	var reqCount atomic.Int32
	var receivedETag atomic.Pointer[string]
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		reqCount.Add(1)
		etag := req.Header.Get("If-None-Match")
		receivedETag.Store(&etag)
		if etag == `"etag-initial"` {
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Body:       io.NopCloser(bytes.NewBufferString("")),
				Header:     make(http.Header),
			}, nil
		}
		header := make(http.Header)
		header.Set("ETag", `"etag-initial"`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`[{"id":1,"app_slug":"app-1"}]`)),
			Header:     header,
		}, nil
	})

	httpClient := &http.Client{Transport: &etagTransport{transport: rt}}
	ghClient, _ := github.NewClient(github.WithHTTPClient(httpClient))
	client := &GHClient{
		EnterpriseSlug: "my-ent",
		Client:         ghClient,
		cache:          newOrgInstallationCache(defaultCacheTTL),
	}

	ctx := context.Background()

	// 1. Initial call populates in-memory cache with ETag "etag-initial"
	insts1, err := client.ListAppInstallationsCached(ctx, "my-org")
	if err != nil || len(insts1) != 1 {
		t.Fatalf("call 1 failed: %v, len: %d", err, len(insts1))
	}

	// Force cache expiration so next call performs HTTP conditional GET
	client.cache.Expire("my-org")

	// 2. Second call sends If-None-Match: "etag-initial" and receives 304 Not Modified
	insts2, err := client.ListAppInstallationsCached(ctx, "my-org")
	if err != nil {
		t.Fatalf("call 2 failed: %v", err)
	}

	if len(insts2) != 1 {
		t.Errorf("expected 1 cached installation on 304 response, got %d", len(insts2))
	}

	gotETag := ""
	if p := receivedETag.Load(); p != nil {
		gotETag = *p
	}
	if gotETag != `"etag-initial"` {
		t.Errorf("expected If-None-Match header %q, got %q", `"etag-initial"`, gotETag)
	}

	if reqCount.Load() != 2 {
		t.Errorf("expected 2 HTTP requests, got %d", reqCount.Load())
	}
}

func TestGHClient_InvalidateOrgCache(t *testing.T) {
	t.Parallel()

	client := &GHClient{
		cache: newOrgInstallationCache(defaultCacheTTL),
	}
	client.cache.Set("org-a", []*github.Installation{{ID: github.Ptr(int64(1))}}, "")

	client.InvalidateOrgCache("org-a")

	_, _, exists := client.cache.Get("org-a")
	if exists {
		t.Errorf("expected org-a cache to be invalidated")
	}
}

func TestOrgInstallationCache_DefensiveCopy(t *testing.T) {
	t.Parallel()

	cache := newOrgInstallationCache(defaultCacheTTL)

	origInst := &github.Installation{
		ID:      github.Ptr(int64(100)),
		AppSlug: github.Ptr("orig-slug"),
		Permissions: &github.InstallationPermissions{
			Actions: github.Ptr("read"),
		},
		Events: []string{"push"},
	}

	// 1. Verify Set defensively copies input
	inputSlice := []*github.Installation{origInst}
	cache.Set("org-copy", inputSlice, "etag-1")

	// Mutate input slice and original struct after Set
	inputSlice[0] = nil
	*origInst.AppSlug = "mutated-slug"
	origInst.Events[0] = "mutated-event"
	*origInst.Permissions.Actions = "write"

	entry, isFresh, exists := cache.Get("org-copy")
	if !exists || !isFresh || len(entry.installations) != 1 {
		t.Fatalf("expected cache entry to exist, got exists=%v, len=%d", exists, len(entry.installations))
	}

	got := entry.installations[0]
	if got.GetAppSlug() != "orig-slug" {
		t.Errorf("cache was corrupted by input struct mutation; got %q, want 'orig-slug'", got.GetAppSlug())
	}
	if len(got.Events) != 1 || got.Events[0] != "push" {
		t.Errorf("cache was corrupted by input slice mutation; got %v, want ['push']", got.Events)
	}
	if got.GetPermissions().GetActions() != "read" {
		t.Errorf("cache was corrupted by input permissions mutation; got %q, want 'read'", got.GetPermissions().GetActions())
	}

	// 2. Verify Get returns a defensive copy that caller cannot mutate
	*got.AppSlug = "mutated-from-get"
	got.Events[0] = "mutated-from-get-event"
	*got.Permissions.Actions = "admin"

	entry2, _, _ := cache.Get("org-copy")
	got2 := entry2.installations[0]
	if got2.GetAppSlug() != "orig-slug" {
		t.Errorf("cache was corrupted by Get output mutation; got %q, want 'orig-slug'", got2.GetAppSlug())
	}
	if len(got2.Events) != 1 || got2.Events[0] != "push" {
		t.Errorf("cache was corrupted by Get events mutation; got %v, want ['push']", got2.Events)
	}
	if got2.GetPermissions().GetActions() != "read" {
		t.Errorf("cache was corrupted by Get permissions mutation; got %q, want 'read'", got2.GetPermissions().GetActions())
	}
}

func TestListAppInstallationsCached_DefensiveCopy(t *testing.T) {
	t.Parallel()

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body := `[{"id":1,"app_slug":"app-1","permissions":{"actions":"read"},"events":["push"]}]`
		header := make(http.Header)
		header.Set("ETag", `"etag-test"`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Header:     header,
		}, nil
	})

	httpClient := &http.Client{Transport: rt}
	ghClient, _ := github.NewClient(github.WithHTTPClient(httpClient))
	client := &GHClient{
		EnterpriseSlug: "ent",
		Client:         ghClient,
		cache:          newOrgInstallationCache(defaultCacheTTL),
	}

	ctx := context.Background()

	// Call 1
	insts1, err := client.ListAppInstallationsCached(ctx, "org-test")
	if err != nil || len(insts1) != 1 {
		t.Fatalf("call 1 failed: %v, len: %d", err, len(insts1))
	}

	// Mutate returned slice & struct
	*insts1[0].AppSlug = "mutated"
	insts1[0].Events[0] = "mutated-event"
	_ = append(insts1, &github.Installation{ID: github.Ptr(int64(999))})
	insts1[0] = nil

	// Call 2 (cache hit)
	insts2, err := client.ListAppInstallationsCached(ctx, "org-test")
	if err != nil || len(insts2) != 1 {
		t.Fatalf("call 2 failed: %v, len: %d", err, len(insts2))
	}

	if insts2[0].GetAppSlug() != "app-1" {
		t.Errorf("expected 'app-1', got %q", insts2[0].GetAppSlug())
	}
	if len(insts2[0].Events) != 1 || insts2[0].Events[0] != "push" {
		t.Errorf("expected ['push'], got %v", insts2[0].Events)
	}
}
