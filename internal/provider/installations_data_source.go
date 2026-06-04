package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var _ datasource.DataSource = &InstallationsDataSource{}
var _ datasource.DataSourceWithConfigure = &InstallationsDataSource{}

// NewInstallationsDataSource is a helper function to simplify the provider implementation.
func NewInstallationsDataSource() datasource.DataSource {
	return &InstallationsDataSource{}
}

// InstallationsDataSource is the data source implementation.
type InstallationsDataSource struct {
	client *GHClient
}

// Metadata returns the data source type name.
func (d *InstallationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_installations"
}

type EachAppModel struct {
	Id                  types.String `tfsdk:"id"` // This is the installation ID of the app
	AppSlug             types.String `tfsdk:"app_slug"`
	ClientId            types.String `tfsdk:"client_id"`
	RepositorySelection types.String `tfsdk:"repository_selection"`
	RepositoriesURL     types.String `tfsdk:"repositories_url"`
	Permissions         types.Map    `tfsdk:"permissions"` // map[string]String
	Events              types.List   `tfsdk:"events"`      // []String
	CreatedAt           types.String `tfsdk:"created_at"`
	UpdatedAt           types.String `tfsdk:"updated_at"`
}
type InstallationsModel struct {
	TargetOrg     types.String   `tfsdk:"target_org"`
	Installations []EachAppModel `tfsdk:"installations"`
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
	var data InstallationsModel

	// Terraform configuration data into data
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Make the request

	installations, _, err := d.client.Client.Enterprise.ListAppInstallations(ctx, d.client.EnterpriseSlug, data.TargetOrg.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list installations", err.Error())
		return
	}

	data.Installations = make([]EachAppModel, len(installations))

	for i, installation := range installations {
		data.Installations[i].Id = types.StringValue(strconv.FormatInt(installation.GetID(), 10))
		data.Installations[i].AppSlug = types.StringValue(installation.GetAppSlug())
		data.Installations[i].ClientId = types.StringValue(installation.GetClientID())
		data.Installations[i].RepositorySelection = types.StringValue(installation.GetRepositorySelection())
		data.Installations[i].RepositoriesURL = types.StringValue(installation.GetRepositoriesURL())
		data.Installations[i].CreatedAt = types.StringValue(installation.GetCreatedAt().String())
		data.Installations[i].UpdatedAt = types.StringValue(installation.GetUpdatedAt().String())

		var permissionsMap map[string]string
		if installation.Permissions != nil {
			pb, err := json.Marshal(installation.Permissions)
			if err == nil {
				_ = json.Unmarshal(pb, &permissionsMap)
			}
		}

		permissionsVal, diags := types.MapValueFrom(ctx, types.StringType, permissionsMap)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Installations[i].Permissions = permissionsVal

		eventsVal, diags := types.ListValueFrom(ctx, types.StringType, installation.Events)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Installations[i].Events = eventsVal
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Error setting state: %+v", resp.Diagnostics.Errors()))
	}
}