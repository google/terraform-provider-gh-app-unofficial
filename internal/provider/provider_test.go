// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"ghapp": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		return
	}

	if os.Getenv("GITHUB_TOKEN") == "" || os.Getenv("GITHUB_ENTERPRISE_SLUG") == "" || os.Getenv("GITHUB_TARGET_ORG") == "" || os.Getenv("GITHUB_APP_CLIENT_ID") == "" {
		t.Fatal("GITHUB_TOKEN, GITHUB_ENTERPRISE_SLUG, GITHUB_TARGET_ORG, and GITHUB_APP_CLIENT_ID must be set for acceptance tests.")
	}
}

func TestProvider(t *testing.T) {
	_ = testAccProtoV6ProviderFactories
}
