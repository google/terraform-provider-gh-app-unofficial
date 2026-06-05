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

type App struct {
	ID                  types.String `tfsdk:"id"` // This is the installation ID of the app
	AppSlug             types.String `tfsdk:"app_slug"`
	ClientID            types.String `tfsdk:"client_id"`
	RepositorySelection types.String `tfsdk:"repository_selection"`
	RepositoriesURL     types.String `tfsdk:"repositories_url"`
	Permissions         types.Map    `tfsdk:"permissions"` // map[string]String
	Events              types.List   `tfsdk:"events"`      // []String
	CreatedAt           types.String `tfsdk:"created_at"`
	UpdatedAt           types.String `tfsdk:"updated_at"`
}
type Installation struct {
	TargetOrg     types.String `tfsdk:"target_org"`
	Installations []App        `tfsdk:"installations"`
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
						"app_slug": schema.StringAttribute{
							MarkdownDescription: "The slug of the app.",
							Computed:            true,
						},
						"client_id": schema.StringAttribute{
							MarkdownDescription: "The client ID of the app.",
							Computed:            true,
						},
						"repository_selection": schema.StringAttribute{
							MarkdownDescription: "The type of repository selection for the app installation.",
							Computed:            true,
						},
						"repositories_url": schema.StringAttribute{
							MarkdownDescription: "The URL for the repositories of the app installation.",
							Computed:            true,
						},
						"permissions": schema.MapAttribute{
							MarkdownDescription: "The permissions for the app installation.",
							Computed:            true,
							ElementType:         types.StringType,
						},
						"events": schema.ListAttribute{
							MarkdownDescription: "The events for the app installation.",
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

func (d *InstallationsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*GHClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *GHClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

// Read refreshes the Terraform state with the latest data.
func (d *InstallationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data Installation

	// Terraform configuration data into data
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Make the request
	client := d.client.Client
	enterpriseSlug := d.client.EnterpriseSlug

	installations, _, err := client.Enterprise.ListAppInstallations(ctx, enterpriseSlug, data.TargetOrg.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list installations", err.Error())
		return
	}

	data.Installations = flattenInstallations(ctx, installations, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Error setting state: %+v", resp.Diagnostics.Errors()))
	}
}

func flattenInstallations(ctx context.Context, installations []*github.Installation, diags *diag.Diagnostics) []App {
	result := make([]App, len(installations))

	for i, installation := range installations {
		result[i].ID = types.StringValue(strconv.FormatInt(installation.GetID(), 10))
		result[i].AppSlug = types.StringValue(installation.GetAppSlug())
		result[i].ClientID = types.StringValue(installation.GetClientID())
		result[i].RepositorySelection = types.StringValue(installation.GetRepositorySelection())
		result[i].RepositoriesURL = types.StringValue(installation.GetRepositoriesURL())
		result[i].CreatedAt = types.StringValue(installation.GetCreatedAt().String())
		result[i].UpdatedAt = types.StringValue(installation.GetUpdatedAt().String())

		var permissionsMap map[string]string
		if installation.Permissions != nil {
			pb, err := json.Marshal(installation.Permissions)
			if err == nil {
				_ = json.Unmarshal(pb, &permissionsMap)
			}
		}

		permissionsVal, errDiags := types.MapValueFrom(ctx, types.StringType, permissionsMap)
		diags.Append(errDiags...)
		if diags.HasError() {
			return nil
		}
		result[i].Permissions = permissionsVal

		eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, installation.Events)
		diags.Append(errDiags...)
		if diags.HasError() {
			return nil
		}
		result[i].Events = eventsVal
	}

	return result
}
