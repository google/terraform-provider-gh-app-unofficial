// Copyright 2026 Google LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// flattenPermissions converts a github.InstallationPermissions struct into a Terraform types.Map
// via a JSON marshal/unmarshal round-trip.
func flattenPermissions(ctx context.Context, permissions *github.InstallationPermissions, diags *diag.Diagnostics) types.Map {
	var permissionsMap map[string]string
	if permissions != nil {
		b, err := json.Marshal(permissions)
		if err != nil {
			diags.AddError(
				"Error Marshalling Permissions",
				fmt.Sprintf("Could not marshal installation permissions: %s", err.Error()),
			)
			return types.MapNull(types.StringType)
		}

		err = json.Unmarshal(b, &permissionsMap)
		if err != nil {
			diags.AddError(
				"Error Unmarshalling Permissions",
				fmt.Sprintf("Could not unmarshal installation permissions: %s", err.Error()),
			)
			return types.MapNull(types.StringType)
		}
	}

	permissionsVal, errDiags := types.MapValueFrom(ctx, types.StringType, permissionsMap)
	diags.Append(errDiags...)
	return permissionsVal
}

// listAllAppInstallationsRaw executes the raw enterprise list installations request and returns installations along with the initial HTTP response.
func listAllAppInstallationsRaw(ctx context.Context, client *github.Client, enterpriseSlug, targetOrg string) ([]*github.Installation, *github.Response, error) {
	var allInstallations []*github.Installation
	opts := &github.ListOptions{PerPage: 100}
	var firstResp *github.Response

	for {
		installations, resp, err := client.Enterprise.ListAppInstallations(ctx, enterpriseSlug, targetOrg, opts)
		if firstResp == nil {
			firstResp = resp
		}
		if err != nil {
			return nil, firstResp, err
		}
		allInstallations = append(allInstallations, installations...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allInstallations, firstResp, nil
}

// listAllAppInstallations lists all GitHub App installations for an organization, paginating through all results using an iterator.
func listAllAppInstallations(ctx context.Context, client *github.Client, enterpriseSlug, targetOrg string) ([]*github.Installation, error) {
	installations, _, err := listAllAppInstallationsRaw(ctx, client, enterpriseSlug, targetOrg)
	return installations, err
}

// listAllRepositoriesForOrgAppInstallation lists all repositories for an organization app installation, paginating through all results using an iterator.
func listAllRepositoriesForOrgAppInstallation(ctx context.Context, client *github.Client, enterpriseSlug, targetOrg string, installationID int64) ([]*github.AccessibleRepository, error) {
	var allRepos []*github.AccessibleRepository
	opts := &github.ListOptions{PerPage: 100}

	for repo, err := range client.Enterprise.ListRepositoriesForOrgAppInstallationIter(ctx, enterpriseSlug, targetOrg, installationID, opts) {
		if err != nil {
			return nil, err
		}
		allRepos = append(allRepos, repo)
	}

	return allRepos, nil
}

// getSelectedRepositories lists repositories for an org app installation when selection is "selected",
// returning their names as a Terraform types.Set. If selection is not "selected", it returns a null set.
func getSelectedRepositories(ctx context.Context, client *github.Client, enterpriseSlug, targetOrg string, installationID int64, selection string, diags *diag.Diagnostics) types.Set {
	if selection != "selected" {
		return types.SetNull(types.StringType)
	}

	repos, err := listAllRepositoriesForOrgAppInstallation(ctx, client, enterpriseSlug, targetOrg, installationID)
	if err != nil {
		tflog.Error(ctx, "Failed to list repositories for installation", map[string]interface{}{
			"error":           err.Error(),
			"installation_id": installationID,
			"enterprise_slug": enterpriseSlug,
			"target_org":      targetOrg,
		})
		diags.AddError(
			"Error Reading GitHub App Installation Repositories",
			fmt.Sprintf("Could not list repositories: %s", err.Error()),
		)
		return types.SetNull(types.StringType)
	}

	var repoNames []string
	for _, repo := range repos {
		repoNames = append(repoNames, repo.GetName())
	}

	selectedReposVal, errDiags := types.SetValueFrom(ctx, types.StringType, repoNames)
	diags.Append(errDiags...)
	return selectedReposVal
}

// isKnown returns true if the attribute value is non-nil, not null, and not unknown.
func isKnown(val attr.Value) bool {
	return val != nil && !val.IsNull() && !val.IsUnknown()
}

// setToStringSlice converts a Terraform types.Set of strings into a Go []string slice.
func setToStringSlice(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	var elements []string
	if isKnown(set) {
		errDiags := set.ElementsAs(ctx, &elements, false)
		diags.Append(errDiags...)
	}
	return elements
}

// getGHClient extracts and type-asserts *GHClient from framework ProviderData.
func getGHClient(ctx context.Context, providerData any, diags *diag.Diagnostics) *GHClient {
	if providerData == nil {
		return nil
	}

	client, ok := providerData.(*GHClient)
	if !ok {
		tflog.Error(ctx, "Unexpected Configure Type", map[string]interface{}{
			"expected": "*GHClient",
			"got":      fmt.Sprintf("%T", providerData),
		})
		diags.AddError(
			"Unexpected Configure Type",
			fmt.Sprintf("Expected *GHClient, got: %T. Please report this issue to the provider developers.", providerData),
		)
		return nil
	}

	return client
}

// parseCompositeID parses an ID string in the format "<target_org>/<installation_id>"
// and returns the target organization, installation numeric ID as int64, installation ID as string, and an error if malformed.
func parseCompositeID(id string) (targetOrg string, instID int64, instIDStr string, err error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", 0, "", fmt.Errorf("ID must be in the format <target_org>/<installation_id>, got: %q", id)
	}

	targetOrg = parts[0]
	instIDStr = parts[1]
	instID, err = strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return "", 0, "", fmt.Errorf("installation ID must be an integer, got: %q (%w)", instIDStr, err)
	}

	return targetOrg, instID, instIDStr, nil
}

const (
	dotComAPIHost   = "api.github.com"
	dotComHost      = "github.com"
	ghesRESTAPIPath = "api/v3/"
)

func formatBaseURL(baseURL string, diags *diag.Diagnostics) string {
	if baseURL == "" {
		diags.AddError("Invalid Base URL", "base URL must not be empty")
		return ""
	}

	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		diags.AddError("Invalid Base URL", fmt.Sprintf("Unable to parse base URL: %s", err))
		return ""
	}

	if u.Scheme != "https" {
		diags.AddError("Invalid Base URL", "base URL must use the https scheme")
		return ""
	}

	// Ensure URL has a trailing slash
	u = u.JoinPath("/")

	switch u.Host {
	case dotComHost:
		u.Host = dotComAPIHost
	case dotComAPIHost:
	default:
		// Assume it's Enterprise Server
		if u.Path == "/" {
			u.Path = ghesRESTAPIPath
		}
	}

	return u.String()
}
