package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccInstallationResource(t *testing.T) {
	enterpriseSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	// We can retrieve a test repository if needed.
	// When repository_selection is "selected", we must provide a real repository name
	// that exists in the target organization for the installation to succeed.
	testRepo := os.Getenv("GITHUB_TEST_REPO")
	if testRepo == "" {
		testRepo = "terraform-provider-gh-app-unofficial" // fallback placeholder
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t); testAccResourcePreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create an installation with all repositories selection
			{
				Config: testAccInstallationResourceConfig_all(enterpriseSlug, targetOrg, clientID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("ghapp_installation.test", "target_org", targetOrg),
					resource.TestCheckResourceAttr("ghapp_installation.test", "client_id", clientID),
					resource.TestCheckResourceAttr("ghapp_installation.test", "repository_selection", "all"),
					resource.TestCheckResourceAttrSet("ghapp_installation.test", "id"),
					resource.TestCheckResourceAttrSet("ghapp_installation.test", "app_slug"),
				),
			},
			// Step 2: Update to selected repositories selection
			{
				Config: testAccInstallationResourceConfig_selected(enterpriseSlug, targetOrg, clientID, testRepo),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("ghapp_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("ghapp_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("ghapp_installation.test", "selected_repositories.0", testRepo),
				),
			},
		},
	})
}

func testAccResourcePreCheck(t *testing.T) {
	if v := os.Getenv("GITHUB_APP_CLIENT_ID"); v == "" {
		t.Fatal("GITHUB_APP_CLIENT_ID must be set for resource acceptance tests")
	}
}

func testAccInstallationResourceConfig_all(enterpriseSlug, targetOrg, clientID string) string {
	return testAccProviderConfig(enterpriseSlug) + fmt.Sprintf(`
resource "ghapp_installation" "test" {
  target_org           = %[1]q
  client_id            = %[2]q
  repository_selection = "all"
}
`, targetOrg, clientID)
}

func testAccInstallationResourceConfig_selected(enterpriseSlug, targetOrg, clientID, repo string) string {
	return testAccProviderConfig(enterpriseSlug) + fmt.Sprintf(`
resource "ghapp_installation" "test" {
  target_org            = %[1]q
  client_id             = %[2]q
  repository_selection  = "selected"
  selected_repositories = [%[3]q]
}
`, targetOrg, clientID, repo)
}
