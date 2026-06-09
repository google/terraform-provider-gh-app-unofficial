package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource              = &installationResource{}
	_ resource.ResourceWithConfigure = &installationResource{}
)

// installationResource is the resource implementation.
type installationResource struct {
	client *GHClient
}

// installationResourceModel describes the resource data model.
type installationResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	TargetOrg            types.String `tfsdk:"target_org"`
	ClientID             types.String `tfsdk:"client_id"`
	AppSlug              types.String `tfsdk:"app_slug"`
	RepositorySelection  types.String `tfsdk:"repository_selection"`
	SelectedRepositories types.List   `tfsdk:"selected_repositories"`
	Permissions          types.Map    `tfsdk:"permissions"`
	Events               types.List   `tfsdk:"events"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

// Metadata returns the resource type name.
func (r *installationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_installation"
}

// Schema defines the schema for the resource.
func (r *installationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The ID of the GitHub App installation.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"target_org": schema.StringAttribute{
				MarkdownDescription: "The organization name where the GitHub App will be installed.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"client_id": schema.StringAttribute{
				Description: "The client ID of the GitHub App to install.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"app_slug": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The slug of the GitHub App.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"repository_selection": schema.StringAttribute{
				Description: "Whether the installation has access to all repositories or only selected ones. Possible values are 'all' or 'selected'.",
				Computed:    true, // It will be computed if it is left out
				Optional:    true,
				Default:     stringdefault.StaticString("all"),
			},
			"selected_repositories": schema.ListAttribute{
				Description: "The list of repository names the installation has access to. Required when repository_selection is 'selected'.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"permissions": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The permissions granted to the installation.",
			},
			"events": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Events the installation is subscribed to.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "The timestamp of when the installation was created.",
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "The timestamp of when the installation was last updated.",
			},
		},
	}
}

func (r *installationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*GHClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *ghClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create creates the resource and sets the initial Terraform state.
func (r *installationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan installationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate the values
	if plan.TargetOrg.IsNull() || plan.TargetOrg.IsUnknown() {
		resp.Diagnostics.AddError(
			"Missing or Unknown Target Organization",
			"The target_org attribute must be set.",
		)
	}

	if plan.ClientID.IsNull() || plan.ClientID.IsUnknown() {
		resp.Diagnostics.AddError(
			"Missing or Unknown Client ID",
			"The client_id attribute must be set.",
		)
	}

	if plan.RepositorySelection.ValueString() == "selected" && (plan.SelectedRepositories.IsNull() || plan.SelectedRepositories.IsUnknown()) {
		resp.Diagnostics.AddError(
			"Missing or Unknown Selected Repositories",
			"The selected_repositories attribute must be set when repository_selection is 'selected'.",
		)
	}

	if !plan.RepositorySelection.IsNull() && !plan.RepositorySelection.IsUnknown() {
		val := plan.RepositorySelection.ValueString()
		if val != "selected" && val != "all" {
			resp.Diagnostics.AddError(
				"Invalid Repository Selection",
				"The repository_selection attribute must be either 'selected' or 'all'.",
			)
		}
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Generate API request body from plan
	client := r.client.Client
	enterpriseSlug := r.client.EnterpriseSlug
	targetOrg := plan.TargetOrg.ValueString()

	var selectedRepos []string
	if !plan.SelectedRepositories.IsNull() && !plan.SelectedRepositories.IsUnknown() {
		diags := plan.SelectedRepositories.ElementsAs(ctx, &selectedRepos, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	repoSelection := "all"
	if !plan.RepositorySelection.IsNull() && !plan.RepositorySelection.IsUnknown() {
		repoSelection = plan.RepositorySelection.ValueString()
	}

	ghReq := github.InstallAppRequest{
		ClientID:            plan.ClientID.ValueString(),
		RepositorySelection: repoSelection,
		Repositories:        selectedRepos,
	}

	installation, _, err := client.Enterprise.InstallApp(ctx, enterpriseSlug, targetOrg, ghReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to install app", err.Error())
		return
	}

	// Map response body to schema and populate Computed attribute values
	var permissionsMap map[string]string
	if installation.Permissions != nil {
		pb, err := json.Marshal(installation.Permissions)
		if err == nil {
			_ = json.Unmarshal(pb, &permissionsMap)
		}
	}
	permissionsVal, errDiags := types.MapValueFrom(ctx, types.StringType, permissionsMap)
	resp.Diagnostics.Append(errDiags...)

	eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, installation.Events)
	resp.Diagnostics.Append(errDiags...)

	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%d", installation.GetID()))
	plan.AppSlug = types.StringValue(installation.GetAppSlug())
	plan.RepositorySelection = types.StringValue(installation.GetRepositorySelection())
	plan.Permissions = permissionsVal
	plan.Events = eventsVal
	plan.CreatedAt = types.StringValue(installation.GetCreatedAt().String())
	plan.UpdatedAt = types.StringValue(installation.GetUpdatedAt().String())

	// Set state to fully populated data
	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Installation created successfully", map[string]interface{}{
		"id": plan.ID.ValueString(),
	})
}

// Read refreshes the Terraform state with the latest data.
func (r *installationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state installationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.client.Client
	enterpriseSlug := r.client.EnterpriseSlug

	// List installations for the organization
	installations, _, err := client.Enterprise.ListAppInstallations(ctx, enterpriseSlug, state.TargetOrg.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading GitHub App Installations",
			fmt.Sprintf("Could not list installations: %s", err.Error()),
		)
		return
	}

	var foundInstallation *github.Installation
	for _, inst := range installations {
		if fmt.Sprintf("%d", inst.GetID()) == state.ID.ValueString() {
			foundInstallation = inst
			break
		}
	}

	if foundInstallation == nil {
		// Resource no longer exists
		resp.State.RemoveResource(ctx)
		return
	}

	// Map response body to state
	var permissionsMap map[string]string
	if foundInstallation.Permissions != nil {
		pb, err := json.Marshal(foundInstallation.Permissions)
		if err == nil {
			_ = json.Unmarshal(pb, &permissionsMap)
		}
	}
	permissionsVal, errDiags := types.MapValueFrom(ctx, types.StringType, permissionsMap)
	resp.Diagnostics.Append(errDiags...)

	eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, foundInstallation.Events)
	resp.Diagnostics.Append(errDiags...)

	if resp.Diagnostics.HasError() {
		return
	}

	// These values are returned by the "Snapshot" Enterprise call
	state.AppSlug = types.StringValue(foundInstallation.GetAppSlug())
	state.RepositorySelection = types.StringValue(foundInstallation.GetRepositorySelection())
	state.Permissions = permissionsVal
	state.Events = eventsVal
	state.CreatedAt = types.StringValue(foundInstallation.GetCreatedAt().String())
	state.UpdatedAt = types.StringValue(foundInstallation.GetUpdatedAt().String())

	// Update selected repositories if selection is "selected"
	var selectedReposVal types.List
	if foundInstallation.GetRepositorySelection() == "selected" {
		repos, _, err := client.Enterprise.ListRepositoriesForOrgAppInstallation(ctx, enterpriseSlug, state.TargetOrg.ValueString(), foundInstallation.GetID(), nil)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading GitHub App Installation Repositories",
				fmt.Sprintf("Could not list repositories: %s", err.Error()),
			)
			return
		}
		var repoNames []string
		for _, repo := range repos {
			repoNames = append(repoNames, repo.GetName())
		}
		var errDiags diag.Diagnostics
		selectedReposVal, errDiags = types.ListValueFrom(ctx, types.StringType, repoNames)
		resp.Diagnostics.Append(errDiags...)
	} else {
		selectedReposVal = types.ListNull(types.StringType)
	}

	if resp.Diagnostics.HasError() {
		return
	}
	state.SelectedRepositories = selectedReposVal

	// Save updated state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *installationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *installationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
