package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var _ datasource.DataSource = &InstallationsDataSource{}
var _ datasource.DataSourceWithConfigure = &InstallationsDataSource{}

// InstallationsDataSource is the data source implementation.
type InstallationsDataSource struct {
	client *GHClient
}

// Metadata returns the data source type name.
func (d *InstallationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_installations"
}

// app represents the model of a single GitHub App Installation in the Terraform state.
type app struct {
	ID                   types.String `tfsdk:"id"` // This is the installation ID of the app
	ClientID             types.String `tfsdk:"client_id"`
	AppSlug              types.String `tfsdk:"app_slug"`
	SelectedRepositories types.List   `tfsdk:"selected_repositories"`
	RepositorySelection  types.String `tfsdk:"repository_selection"`
	Events               types.List   `tfsdk:"events"`      // []String
	Permissions          types.Map    `tfsdk:"permissions"` // map[string]String
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

// installation represents the data source model containing the target organization and its list of app installations.
type installation struct {
	TargetOrg     types.String `tfsdk:"target_org"`
	Installations []app        `tfsdk:"installations"`
}

// Schema defines the schema for the data source.
func (d *InstallationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"target_org": schema.StringAttribute{
				Optional:    true,
				Description: "The organization name for which to list installations.",
			},
			"installations": schema.ListNestedAttribute{
				Description: "List of all Apps installed in target_org",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "The ID of the app installation.",
							Computed:            true,
						},
						"client_id": schema.StringAttribute{
							MarkdownDescription: "The client ID of the app.",
							Computed:            true,
						},
						"app_slug": schema.StringAttribute{
							MarkdownDescription: "The slug of the app.",
							Computed:            true,
						},
						"selected_repositories": schema.ListAttribute{
							MarkdownDescription: "The list of repository names the installation has access to.",
							Computed:            true,
							ElementType:         types.StringType,
						},
						"repository_selection": schema.StringAttribute{
							MarkdownDescription: "The type of repository selection for the app installation.",
							Computed:            true,
						},
						"events": schema.ListAttribute{
							MarkdownDescription: "The events for the app installation.",
							Computed:            true,
							ElementType:         types.StringType,
						},
						"permissions": schema.MapAttribute{
							MarkdownDescription: "The permissions for the app installation.",
							Computed:            true,
							ElementType:         types.StringType,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "The creation timestamp of the app installation.",
							Computed:            true,
						},
						"updated_at": schema.StringAttribute{
							MarkdownDescription: "The update timestamp of the app installation.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider-configured GitHub client to the data source.
func (d *InstallationsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured. req.ProviderData
	// can be nil during early lifecycle phases (such as terraform validate or
	// initial schema discovery). Returning early without adding a diagnostic
	// error allows the framework to proceed safely.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*GHClient)

	if !ok {
		tflog.Error(ctx, "Unexpected Data Source Configure Type", map[string]interface{}{
			"expected": "*GHClient",
			"got":      fmt.Sprintf("%T", req.ProviderData),
		})
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *GHClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client

	tflog.Info(ctx, "Configured installations data source client", map[string]interface{}{
		"enterprise_slug": client.EnterpriseSlug,
	})
}

// Read refreshes the Terraform state with the latest data.
func (d *InstallationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data installation

	// Terraform configuration data into data
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Reading GitHub App installations", map[string]interface{}{
		"target_org": data.TargetOrg.ValueString(),
	})

	// Make the request
	client := d.client.Client
	enterpriseSlug := d.client.EnterpriseSlug

	installations, _, err := client.Enterprise.ListAppInstallations(ctx, enterpriseSlug, data.TargetOrg.ValueString(), nil)
	if err != nil {
		tflog.Error(ctx, "Failed to list installations", map[string]interface{}{
			"error":           err.Error(),
			"enterprise_slug": enterpriseSlug,
			"target_org":      data.TargetOrg.ValueString(),
		})
		resp.Diagnostics.AddError("Failed to list installations", err.Error())
		return
	}

	data.Installations = flattenInstallations(ctx, client, enterpriseSlug, data.TargetOrg.ValueString(), installations, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Error setting state: %+v", resp.Diagnostics.Errors()))
		return
	}

	tflog.Info(ctx, "Finished reading GitHub App installations", map[string]interface{}{
		"target_org": data.TargetOrg.ValueString(),
		"count":      len(installations),
	})
}

func flattenInstallations(ctx context.Context, client *github.Client, enterpriseSlug, targetOrg string, installations []*github.Installation, diags *diag.Diagnostics) []app {
	result := make([]app, 0, len(installations))

	for _, installation := range installations {
		var permissionsMap map[string]string
		if installation.Permissions != nil {
			// Dynamically convert the InstallationPermissions struct to map[string]string through a JSON marshal/unmarshal round-trip
			pb, err := json.Marshal(installation.Permissions)
			if err != nil {
				diags.AddError(
					"Error Marshalling Permissions",
					fmt.Sprintf("Could not marshal installation permissions for installation ID %d: %s", installation.GetID(), err.Error()),
				)
				return nil
			}
			err = json.Unmarshal(pb, &permissionsMap)
			if err != nil {
				diags.AddError(
					"Error Unmarshalling Permissions",
					fmt.Sprintf("Could not unmarshal installation permissions for installation ID %d: %s", installation.GetID(), err.Error()),
				)
				return nil
			}
		}

		permissionsVal, errDiags := types.MapValueFrom(ctx, types.StringType, permissionsMap)
		diags.Append(errDiags...)
		if diags.HasError() {
			return nil
		}

		eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, installation.Events)
		diags.Append(errDiags...)
		if diags.HasError() {
			return nil
		}

		var selectedReposVal types.List
		if installation.GetRepositorySelection() == "selected" {
			repos, _, err := client.Enterprise.ListRepositoriesForOrgAppInstallation(ctx, enterpriseSlug, targetOrg, installation.GetID(), nil)
			if err != nil {
				tflog.Error(ctx, "Failed to list repositories for installation", map[string]interface{}{
					"error":           err.Error(),
					"installation_id": installation.GetID(),
					"enterprise_slug": enterpriseSlug,
					"target_org":      targetOrg,
				})
				diags.AddError(
					"Error Reading GitHub App Installation Repositories",
					fmt.Sprintf("Could not list repositories for installation ID %d: %s", installation.GetID(), err.Error()),
				)
				return nil
			}
			var repoNames []string
			for _, repo := range repos {
				repoNames = append(repoNames, repo.GetName())
			}
			var errDiags diag.Diagnostics
			selectedReposVal, errDiags = types.ListValueFrom(ctx, types.StringType, repoNames)
			diags.Append(errDiags...)
			if diags.HasError() {
				return nil
			}
		} else {
			selectedReposVal = types.ListNull(types.StringType)
		}

		result = append(result, app{
			ID:                   types.StringValue(strconv.FormatInt(installation.GetID(), 10)),
			ClientID:             types.StringValue(installation.GetClientID()),
			AppSlug:              types.StringValue(installation.GetAppSlug()),
			SelectedRepositories: selectedReposVal,
			RepositorySelection:  types.StringValue(installation.GetRepositorySelection()),
			Events:               eventsVal,
			Permissions:          permissionsVal,
			CreatedAt:            types.StringValue(installation.GetCreatedAt().String()),
			UpdatedAt:            types.StringValue(installation.GetUpdatedAt().String()),
		})
	}

	return result
}
