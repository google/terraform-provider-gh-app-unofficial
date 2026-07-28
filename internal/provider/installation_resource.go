// Copyright 2026 Google LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-github/v88/github"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &installationResource{}
	_ resource.ResourceWithConfigure      = &installationResource{}
	_ resource.ResourceWithImportState    = &installationResource{}
	_ resource.ResourceWithValidateConfig = &installationResource{}
	_ resource.ResourceWithModifyPlan     = &installationResource{}
)

// installationResource is the resource implementation.
type installationResource struct {
	client *GHClient
}

// installationResourceModel describes the resource data model.
type installationResourceModel struct {
	ID                        types.String `tfsdk:"id"`
	InstallationID            types.String `tfsdk:"installation_id"`
	TargetOrg                 types.String `tfsdk:"target_org"`
	ClientID                  types.String `tfsdk:"client_id"`
	AppSlug                   types.String `tfsdk:"app_slug"`
	SelectedRepositories      types.Set    `tfsdk:"selected_repositories"`
	RepositorySelection       types.String `tfsdk:"repository_selection"`
	Events                    types.List   `tfsdk:"events"`
	Permissions               types.Map    `tfsdk:"permissions"`
	AutoAcceptPermissionDrift types.Bool   `tfsdk:"auto_accept_permission_drift"`
	CreatedAt                 types.String `tfsdk:"created_at"`
	UpdatedAt                 types.String `tfsdk:"updated_at"`
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
				MarkdownDescription: "The composite ID of the app installation in the format `<target_org>/<installation_id>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"installation_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The numeric ID of the app installation.",
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
				MarkdownDescription: "The client ID of the app.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"app_slug": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The slug of the app.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"selected_repositories": schema.SetAttribute{
				MarkdownDescription: "The list of repository names the installation has access to. Only valid and required when repository_selection is set to 'selected'.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"repository_selection": schema.StringAttribute{
				MarkdownDescription: "The type of repository selection for the app installation. Either set to 'all' or 'selected'.",
				Computed:            true,
				Optional:            true,
				Default:             stringdefault.StaticString("all"),
				Validators: []validator.String{
					stringvalidator.OneOf("selected", "all"),
				},
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
			"auto_accept_permission_drift": schema.BoolAttribute{
				MarkdownDescription: "Whether to automatically accept permission changes (drift) on the GitHub App installation when requested permissions change on the GitHub App definition. Defaults to `false`. When `false`, Terraform tracks active installed permissions and expects an Organization Owner to accept pending permission requests via GitHub UI. When `true`, Terraform will detect permission changes on the app definition and automatically accept them by re-running the app installation API call during update.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The creation timestamp of the app installation.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The update timestamp of the app installation.",
			},
		},
	}
}

func (r *installationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := getGHClient(ctx, req.ProviderData, &resp.Diagnostics)
	if client == nil {
		return
	}

	r.client = client
}

// ValidateConfig runs during validation and planning (e.g., `terraform validate` and `terraform plan`).
// Note: If attributes are Unknown at this stage (e.g., if they reference other resources), cross-attribute
// validation is skipped here and instead validated at apply time by the remote API.
func (r *installationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config installationResourceModel

	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Determine the effective repository selection (handling the default of "all" if Null)
	var repoSelection string
	if config.RepositorySelection.IsUnknown() {
		// If it's unknown, we can't perform cross-attribute validation yet.
		return
	} else if config.RepositorySelection.IsNull() {
		// Default value from schema
		repoSelection = "all"
	} else {
		repoSelection = config.RepositorySelection.ValueString()
	}

	// Cross-validation between selected_repositories and repository_selection
	switch repoSelection {
	case "selected":
		if config.SelectedRepositories.IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root("selected_repositories"),
				"Missing Selected Repositories",
				"The selected_repositories attribute must be set when repository_selection is 'selected'.",
			)
		} else if !config.SelectedRepositories.IsUnknown() && len(config.SelectedRepositories.Elements()) == 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root("selected_repositories"),
				"Empty Selected Repositories",
				"The selected_repositories attribute must contain at least one repository when repository_selection is 'selected'.",
			)
		}

	case "all":
		if isKnown(config.SelectedRepositories) && len(config.SelectedRepositories.Elements()) > 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root("selected_repositories"),
				"Invalid Selected Repositories",
				"The selected_repositories attribute must not be set when repository_selection is 'all'.",
			)
		}
	}
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

	// Generate API request body from plan
	client := r.client.Client
	enterpriseSlug := r.client.EnterpriseSlug
	targetOrg := plan.TargetOrg.ValueString()

	// Slice of strings of repositories to install app in
	selectedRepos := setToStringSlice(ctx, plan.SelectedRepositories, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	repoSelection := "all"
	if isKnown(plan.RepositorySelection) {
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
	permissionsVal := flattenPermissions(ctx, installation.GetPermissions(), &resp.Diagnostics)

	eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, installation.GetEvents())
	resp.Diagnostics.Append(errDiags...)

	if resp.Diagnostics.HasError() {
		return
	}

	instIDStr := fmt.Sprintf("%d", installation.GetID())
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s", targetOrg, instIDStr))
	plan.InstallationID = types.StringValue(instIDStr)
	plan.AppSlug = types.StringValue(installation.GetAppSlug())
	plan.RepositorySelection = types.StringValue(installation.GetRepositorySelection())
	plan.Permissions = permissionsVal
	plan.Events = eventsVal
	plan.CreatedAt = types.StringValue(installation.GetCreatedAt().Format(time.RFC3339))
	plan.UpdatedAt = types.StringValue(installation.GetUpdatedAt().Format(time.RFC3339))

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

	targetOrg, _, instIDStr, err := parseCompositeID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unexpected Identifier", err.Error())
		return
	}

	// List installations for the organization
	installations, err := listAllAppInstallations(ctx, client, enterpriseSlug, targetOrg)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading GitHub App Installations",
			fmt.Sprintf("Could not list installations: %s", err.Error()),
		)
		return
	}

	var foundInstallation *github.Installation
	for _, inst := range installations {
		if fmt.Sprintf("%d", inst.GetID()) == instIDStr {
			foundInstallation = inst
			break
		}
	}

	if foundInstallation == nil {
		// Resource no longer exists
		resp.State.RemoveResource(ctx)
		return
	}

	// Map response body (active installed permissions) to state
	permissionsVal := flattenPermissions(ctx, foundInstallation.GetPermissions(), &resp.Diagnostics)

	eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, foundInstallation.GetEvents())
	resp.Diagnostics.Append(errDiags...)

	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(fmt.Sprintf("%s/%s", targetOrg, instIDStr))
	state.InstallationID = types.StringValue(instIDStr)
	state.TargetOrg = types.StringValue(targetOrg)
	state.AppSlug = types.StringValue(foundInstallation.GetAppSlug())
	state.RepositorySelection = types.StringValue(foundInstallation.GetRepositorySelection())
	state.Permissions = permissionsVal
	state.Events = eventsVal
	state.CreatedAt = types.StringValue(foundInstallation.GetCreatedAt().Format(time.RFC3339))
	state.UpdatedAt = types.StringValue(foundInstallation.GetUpdatedAt().Format(time.RFC3339))
	state.ClientID = types.StringValue(foundInstallation.GetClientID())

	// Update selected repositories if selection is "selected"
	selectedReposVal := getSelectedRepositories(ctx, client, enterpriseSlug, targetOrg, foundInstallation.GetID(), foundInstallation.GetRepositorySelection(), &resp.Diagnostics)

	if resp.Diagnostics.HasError() {
		return
	}
	state.SelectedRepositories = selectedReposVal

	if !isKnown(state.AutoAcceptPermissionDrift) {
		state.AutoAcceptPermissionDrift = types.BoolValue(false)
	}

	// Save updated state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *installationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan installationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	targetOrg, instID, _, err := parseCompositeID(plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unexpected Identifier", err.Error())
		return
	}

	client := r.client.Client
	enterpriseSlug := r.client.EnterpriseSlug

	selectedRepos := setToStringSlice(ctx, plan.SelectedRepositories, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	repoSelection := "all"
	if isKnown(plan.RepositorySelection) {
		repoSelection = plan.RepositorySelection.ValueString()
	}

	if isKnown(plan.AutoAcceptPermissionDrift) && plan.AutoAcceptPermissionDrift.ValueBool() {
		ghReq := github.InstallAppRequest{
			ClientID:            plan.ClientID.ValueString(),
			RepositorySelection: repoSelection,
			Repositories:        selectedRepos,
		}

		_, _, err := client.Enterprise.InstallApp(ctx, enterpriseSlug, targetOrg, ghReq)
		if err != nil {
			resp.Diagnostics.AddError("Failed to auto-accept permission drift via app installation", err.Error())
			return
		}
	}

	opts := github.UpdateAppInstallationRepositoriesRequest{
		RepositorySelection: &repoSelection,
		Repositories:        selectedRepos,
	}

	installation, _, err := client.Enterprise.UpdateAppInstallationRepositories(ctx, enterpriseSlug, targetOrg, instID, opts)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update app installation repositories", err.Error())
		return
	}

	if installation == nil {
		resp.Diagnostics.AddError("Failed to update app installation", "Installation not found")
		return
	}

	// Map response body to schema and populate Computed attribute values
	permissionsVal := flattenPermissions(ctx, installation.GetPermissions(), &resp.Diagnostics)

	eventsVal, errDiags := types.ListValueFrom(ctx, types.StringType, installation.GetEvents())
	resp.Diagnostics.Append(errDiags...)

	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s/%d", targetOrg, instID))
	plan.InstallationID = types.StringValue(fmt.Sprintf("%d", instID))
	plan.TargetOrg = types.StringValue(targetOrg)
	plan.AppSlug = types.StringValue(installation.GetAppSlug())
	plan.RepositorySelection = types.StringValue(installation.GetRepositorySelection())
	plan.Permissions = permissionsVal
	plan.Events = eventsVal
	plan.CreatedAt = types.StringValue(installation.GetCreatedAt().Format(time.RFC3339))
	plan.UpdatedAt = types.StringValue(installation.GetUpdatedAt().Format(time.RFC3339))

	// Set state to fully populated data
	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Installation updated successfully", map[string]interface{}{
		"id": plan.ID.ValueString(),
	})
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *installationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state installationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.client.Client
	enterpriseSlug := r.client.EnterpriseSlug

	targetOrg, instID, _, err := parseCompositeID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unexpected Identifier", err.Error())
		return
	}

	_, err = client.Enterprise.UninstallApp(ctx, enterpriseSlug, targetOrg, instID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to uninstall app", err.Error())
		return
	}
}

// ImportState handles the import of an existing resource. Expects <id> in the format <org>/<installation_id>.
func (r *installationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ModifyPlan handles custom plan modifications, updating plan permissions if App definition permissions differ from installed permissions
// AND auto_accept_permission_drift is set to true.
func (r *installationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Skip during resource creation or deletion
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var planModel installationResourceModel
	diags := req.Plan.Get(ctx, &planModel)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var stateModel installationResourceModel
	diags = req.State.Get(ctx, &stateModel)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Mark updated_at as unknown if any configured attribute is changing during update
	if !planModel.SelectedRepositories.Equal(stateModel.SelectedRepositories) ||
		!planModel.RepositorySelection.Equal(stateModel.RepositorySelection) ||
		!planModel.AutoAcceptPermissionDrift.Equal(stateModel.AutoAcceptPermissionDrift) {
		resp.Plan.SetAttribute(ctx, path.Root("updated_at"), types.StringUnknown())
	}

	// Only trigger permission update if auto_accept_permission_drift is true
	if !isKnown(planModel.AutoAcceptPermissionDrift) || !planModel.AutoAcceptPermissionDrift.ValueBool() {
		return
	}

	appSlug := stateModel.AppSlug.ValueString()
	if appSlug == "" || r.client == nil || r.client.Client == nil {
		return
	}

	// Fetch GitHub App definition permissions
	client := r.client.Client
	app, _, err := client.Apps.Get(ctx, appSlug)
	if err != nil {
		tflog.Warn(ctx, "Failed to fetch GitHub App definition in ModifyPlan", map[string]interface{}{
			"app_slug": appSlug,
			"error":    err.Error(),
		})
		return
	}

	appPermissionsVal := flattenPermissions(ctx, app.GetPermissions(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Compare active installed permissions (from state) with App definition permissions
	if isKnown(stateModel.Permissions) && isKnown(appPermissionsVal) {
		if !stateModel.Permissions.Equal(appPermissionsVal) {
			resp.Plan.SetAttribute(ctx, path.Root("permissions"), appPermissionsVal)
			resp.Plan.SetAttribute(ctx, path.Root("events"), types.ListUnknown(types.StringType))
			resp.Plan.SetAttribute(ctx, path.Root("updated_at"), types.StringUnknown())
		}
	}
}
