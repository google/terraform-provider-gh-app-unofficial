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

// getSelectedRepositories lists repositories for an org app installation when selection is "selected",
// returning their names as a Terraform types.List. If selection is not "selected", it returns a null list.
func getSelectedRepositories(ctx context.Context, client *github.Client, enterpriseSlug, targetOrg string, installationID int64, selection string, diags *diag.Diagnostics) types.List {
	if selection != "selected" {
		return types.ListNull(types.StringType)
	}

	repos, _, err := client.Enterprise.ListRepositoriesForOrgAppInstallation(ctx, enterpriseSlug, targetOrg, installationID, nil)
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
		return types.ListNull(types.StringType)
	}

	var repoNames []string
	for _, repo := range repos {
		repoNames = append(repoNames, repo.GetName())
	}

	selectedReposVal, errDiags := types.ListValueFrom(ctx, types.StringType, repoNames)
	diags.Append(errDiags...)
	return selectedReposVal
}

// isKnown returns true if the attribute value is non-nil, not null, and not unknown.
func isKnown(val attr.Value) bool {
	return val != nil && !val.IsNull() && !val.IsUnknown()
}

// listToStringSlice converts a Terraform types.List of strings into a Go []string slice.
func listToStringSlice(ctx context.Context, list types.List, diags *diag.Diagnostics) []string {
	var elements []string
	if isKnown(list) {
		errDiags := list.ElementsAs(ctx, &elements, false)
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

func formatBaseURL(baseURL string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("base URL must not be empty")
	}

	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	if u.Scheme != "https" {
		return "", fmt.Errorf("base URL must use the https scheme")
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

	return u.String(), nil

}
