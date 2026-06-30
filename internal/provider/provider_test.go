// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

const (
	providerConfig = `
provider "ghapp" {
	
}
`
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"ghapp": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck validates that the required environment variables are set before running acceptance tests.
func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("GITHUB_TOKEN"); v == "" {
		t.Fatal("GITHUB_TOKEN must be set for acceptance tests")
	}
	if v := os.Getenv("GITHUB_ENTERPRISE_SLUG"); v == "" {
		t.Fatal("GITHUB_ENTERPRISE_SLUG must be set for acceptance tests")
	}
	if v := os.Getenv("GITHUB_TARGET_ORG"); v == "" {
		t.Fatal("GITHUB_TARGET_ORG must be set for acceptance tests")
	}
}

// testAccProviderConfig returns a basic provider configuration string.
func testAccProviderConfig(enterpriseSlug string) string {
	return fmt.Sprintf(`
provider "ghapp" {
  enterprise_slug = %[1]q
}
`, enterpriseSlug)
}

func TestProvider(t *testing.T) {
	_ = testAccProtoV6ProviderFactories
	_ = providerConfig
	// Minimal check to ensure the provider can be instantiated.
}
