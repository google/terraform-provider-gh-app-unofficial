// Copyright 2026 Google LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// init registers a Terraform test sweeper for "gh-app-unofficial_installation" resources.
// Sweepers clean up dangling or orphaned live resources left behind if acceptance tests fail or crash before destroying resources.
// Sweepers can be executed on demand using `go test -v -sweep=all ./internal/provider/...`.
func init() {
	resource.AddTestSweepers("gh-app-unofficial_installation", &resource.Sweeper{
		Name: "gh-app-unofficial_installation",
		F: func(region string) error {
			ctx := context.Background()
			return sweepInstallations(ctx)
		},
	})
}

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func sweepInstallations(ctx context.Context) error {
	token := os.Getenv("GITHUB_TOKEN")
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	if token == "" || entSlug == "" || targetOrg == "" || clientID == "" {
		return nil
	}

	client, err := github.NewClient(github.WithAuthToken(token))
	if err != nil {
		return fmt.Errorf("failed to create client in sweeper: %w", err)
	}

	installations, err := listAllAppInstallations(ctx, client, entSlug, targetOrg)
	if err != nil {
		return fmt.Errorf("failed to list organization installations in sweeper: %w", err)
	}

	for _, inst := range installations {
		if inst.GetClientID() == clientID && inst.ID != nil {
			_, err := client.Enterprise.UninstallApp(ctx, entSlug, targetOrg, inst.GetID())
			if err != nil {
				return fmt.Errorf("failed to sweep app installation %d: %w", inst.GetID(), err)
			}
		}
	}

	return nil
}

func TestAccInstallationResource(t *testing.T) {
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstallationDestroy,
		Steps: []resource.TestStep{
			// Step 1: Create with selected repositories ("test-repo-1")
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "target_org", targetOrg),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "client_id", clientID),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.0", "test-repo-1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "auto_accept_permission_drift", "false"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "id"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "installation_id"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "created_at"),
					resource.TestCheckResourceAttrSet("gh-app-unofficial_installation.test", "updated_at"),
				),
			},
			// Step 2: Idempotency Verification on Create
			{
				Config:   testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				PlanOnly: true,
			},
			// Step 3: Import State Verification
			{
				ResourceName:            "gh-app-unofficial_installation.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"updated_at"},
			},
			// Step 4: Update - Repository Swap (to "test-repo-2")
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-2"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.0", "test-repo-2"),
				),
			},
			// Step 5: Idempotency Verification on Swap
			{
				Config:   testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-2"`),
				PlanOnly: true,
			},
			// Step 6: Update - Multi-Repository Expansion ("test-repo-1", "test-repo-2")
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1", "test-repo-2"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "2"),
				),
			},
			// Step 7: Update - Toggle Selection Mode to "all"
			{
				Config: testAccInstallationConfig_all(entSlug, targetOrg, clientID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "all"),
					resource.TestCheckNoResourceAttr("gh-app-unofficial_installation.test", "selected_repositories"),
				),
			},
			// Step 8: Idempotency Verification on Mode Toggle
			{
				Config:   testAccInstallationConfig_all(entSlug, targetOrg, clientID),
				PlanOnly: true,
			},
			// Step 9: Destroy & Re-Install Verification
			{
				Config:  testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Destroy: true,
			},
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "repository_selection", "selected"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.0", "test-repo-1"),
				),
			},
		},
	})
}

func TestAccInstallationResource_Drift(t *testing.T) {
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstallationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "1"),
				),
			},
			{
				// Out-of-band modification simulating external drift or refresh behavior
				PreConfig: func() {
					ctx := context.Background()
					token := os.Getenv("GITHUB_TOKEN")
					client, err := github.NewClient(github.WithAuthToken(token))
					if err != nil {
						t.Fatalf("PreConfig NewClient error: %v", err)
					}
					insts, _, err := client.Enterprise.ListAppInstallations(ctx, entSlug, targetOrg, nil)
					if err != nil {
						t.Fatalf("PreConfig ListAppInstallations error: %v", err)
					}
					found := false
					for _, inst := range insts {
						if inst.ClientID != nil && *inst.ClientID == clientID && inst.ID != nil {
							found = true
							opts := github.UpdateAppInstallationRepositoriesRequest{
								RepositorySelection: github.Ptr("selected"),
								Repositories:        []string{"test-repo-1", "test-repo-2"},
							}
							_, _, err := client.Enterprise.UpdateAppInstallationRepositories(ctx, entSlug, targetOrg, *inst.ID, opts)
							if err != nil {
								t.Fatalf("PreConfig UpdateAppInstallationRepositories error on inst %d: %v", *inst.ID, err)
							}
							t.Logf("PreConfig successfully updated inst %d to repositories %v", *inst.ID, opts.Repositories)
						}
					}
					if !found {
						t.Fatalf("PreConfig error: installation with client_id %s not found", clientID)
					}
				},
				Config: testAccInstallationConfig_selected(entSlug, targetOrg, clientID, `"test-repo-1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gh-app-unofficial_installation.test", "selected_repositories.#", "2"),
				),
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccInstallationResource_Unhappy(t *testing.T) {
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstallationConfig_selected(entSlug, "non-existent-sandbox-org-99999", "invalid-client-id-abc", `"test-repo-1"`),
				ExpectError: regexp.MustCompile("Failed to install app|installation not found|error"),
			},
		},
	})
}

func testAccCheckInstallationDestroy(s *terraform.State) error {
	ctx := context.Background()
	token := os.Getenv("GITHUB_TOKEN")
	entSlug := os.Getenv("GITHUB_ENTERPRISE_SLUG")
	targetOrg := os.Getenv("GITHUB_TARGET_ORG")
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")

	if token == "" || targetOrg == "" || clientID == "" {
		return nil
	}

	client, err := github.NewClient(github.WithAuthToken(token))
	if err != nil {
		return fmt.Errorf("error creating github client on CheckDestroy: %w", err)
	}
	insts, _, err := client.Enterprise.ListAppInstallations(ctx, entSlug, targetOrg, nil)
	if err != nil {
		return fmt.Errorf("error listing installations on CheckDestroy: %w", err)
	}

	for _, inst := range insts {
		if inst.ClientID != nil && *inst.ClientID == clientID {
			return fmt.Errorf("installation with client_id %s still exists after destroy", clientID)
		}
	}

	return nil
}

func testAccInstallationConfig_selected(enterpriseSlug, targetOrg, clientID, repos string) string {
	return fmt.Sprintf(`
provider "gh-app-unofficial" {
  enterprise_slug = %[1]q
}

resource "gh-app-unofficial_installation" "test" {
  target_org            = %[2]q
  client_id             = %[3]q
  repository_selection  = "selected"
  selected_repositories = [%[4]s]
}
`, enterpriseSlug, targetOrg, clientID, repos)
}

func testAccInstallationConfig_all(enterpriseSlug, targetOrg, clientID string) string {
	return fmt.Sprintf(`
provider "gh-app-unofficial" {
  enterprise_slug = %[1]q
}

resource "gh-app-unofficial_installation" "test" {
  target_org           = %[2]q
  client_id            = %[3]q
  repository_selection = "all"
}
`, enterpriseSlug, targetOrg, clientID)
}
