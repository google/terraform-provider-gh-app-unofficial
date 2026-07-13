package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func diffErrString(diags diag.Diagnostics, wantErr string) string {
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

// checkDiagnostics asserts that diags contains (or doesn't contain) the expected error substring.
func checkDiagnostics(t *testing.T, diags diag.Diagnostics, wantErr string) {
	t.Helper()
	if diff := diffErrString(diags, wantErr); diff != "" {
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
