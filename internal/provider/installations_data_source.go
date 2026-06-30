package provider

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var _ datasource.DataSource = &installationsDataSource{}
var _ datasource.DataSourceWithConfigure = &installationsDataSource{}

// installationsDataSource is the data source implementation.
type installationsDataSource struct {
	client *GHClient
}

// Metadata returns the data source type name.
func (d *installationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
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
func (d *installationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
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
func (d *installationsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client := getGHClient(ctx, req.ProviderData, &resp.Diagnostics)
	if client == nil {
		return
	}

	d.client = client

	tflog.Info(ctx, "Configured installations data source client", map[string]interface{}{
		"enterprise_slug": client.EnterpriseSlug,
	})
}

// Read refreshes the Terraform state with the latest data.
func (d *installationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
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
		permissionsVal := flattenPermissions(ctx, installation.GetPermissions(), diags)
		if diags.HasError() {
			return nil
		}

		eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, installation.GetEvents())
		diags.Append(errDiags...)
		if diags.HasError() {
			return nil
		}

		selectedReposVal := getSelectedRepositories(ctx, client, enterpriseSlug, targetOrg, installation.GetID(), installation.GetRepositorySelection(), diags)
		if diags.HasError() {
			return nil
		}

		result = append(result, app{
			ID:                   types.StringValue(strconv.FormatInt(installation.GetID(), 10)),
			ClientID:             types.StringValue(installation.GetClientID()),
			AppSlug:              types.StringValue(installation.GetAppSlug()),
			SelectedRepositories: selectedReposVal,
			RepositorySelection:  types.StringValue(installation.GetRepositorySelection()),
			Events:               eventsVal,
			Permissions:          permissionsVal,
			CreatedAt:            types.StringValue(installation.GetCreatedAt().Format(time.RFC3339)),
			UpdatedAt:            types.StringValue(installation.GetUpdatedAt().Format(time.RFC3339)),
		})
	}

	return result
}
