package provider

import (
	"context"
  "fmt"
  "net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var _ datasource.DataSource = &InstallationsDataSource{}

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

type InstallationsModel struct {
  TargetOrg types.String `tfsdk:"target_org"`
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

// Schema defines the schema for the data source.
func (d *InstallationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
      "target_org": schema.StringAttribute{
        Optional: true,
        Description: "The organization name for which to list installations.",
      },
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
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *InstallationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
  var data InstallationsDataSource
  
  resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
  
  if resp.Diagnostics.HasError() {
    return
  }

  url := fmt.Sprintf("enterprises/%s/apps/%s/installations", data.client.EnterpriseSlug, data.TargetOrg.ValueString())

  req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
  if err != nil {
    resp.Diagnostics.AddError("Failed to create request", err.Error())
    return
  }

  req.Header.Set("Authorization", "token "+d.client.Token)

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