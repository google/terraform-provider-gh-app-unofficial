package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestInstallationResourceValidation(t *testing.T) {
	// Note: We do NOT define PreCheck here. This allows the validation tests
	// to run completely offline without needing any GITHUB_TOKEN or enterprise credentials.
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Case 1: repository_selection is 'selected' but selected_repositories is omitted (Null)
			{
				Config:      testAccInstallationResourceValidationConfig_missingRepos,
				ExpectError: regexp.MustCompile("Missing Selected Repositories"),
			},
			// Case 2: repository_selection is 'selected' but selected_repositories is empty ([])
			{
				Config:      testAccInstallationResourceValidationConfig_emptyRepos,
				ExpectError: regexp.MustCompile("Empty Selected Repositories"),
			},
			// Case 3: repository_selection is 'all' but selected_repositories is provided
			{
				Config:      testAccInstallationResourceValidationConfig_invalidAllRepos,
				ExpectError: regexp.MustCompile("Invalid Selected Repositories"),
			},
		},
	})
}

const testAccInstallationResourceValidationConfig_missingRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org           = "test-org"
  client_id            = "test-client-id"
  repository_selection = "selected"
  # selected_repositories is omitted
}
`

const testAccInstallationResourceValidationConfig_emptyRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org            = "test-org"
  client_id             = "test-client-id"
  repository_selection  = "selected"
  selected_repositories = []
}
`

const testAccInstallationResourceValidationConfig_invalidAllRepos = `
provider "ghapp" {
  enterprise_slug = "test-ent"
  token           = "dummy-token"
}

resource "ghapp_installation" "test" {
  target_org            = "test-org"
  client_id             = "test-client-id"
  repository_selection  = "all"
  selected_repositories = ["repo-1"]
}
`
