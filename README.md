# Terraform Provider for GitHub Apps (Unofficial)

A Terraform provider designed to manage GitHub App installations and list installation configurations. This provider is built using the modern [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework).

Using this provider, you can automate the installation and organization-level configuration of GitHub Apps, making it easier to manage app integrations dynamically across your enterprises and organizations.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.24

## Building the Provider

1. Clone the repository
1. Enter the repository directory
1. Build the provider using the Go `install` command:

```shell
go install
```

## Adding Dependencies

This provider uses [Go modules](https://github.com/golang/go/wiki/Modules).
Please see the Go documentation for the most up to date information about using Go modules.

To add a new dependency `github.com/author/dependency` to your Terraform provider:

```shell
go get github.com/author/dependency
go mod tidy
```

Then commit the changes to `go.mod` and `go.sum`.

## Using the Provider

To configure the provider in your Terraform configurations, add the following block:

```hcl
terraform {
  required_providers {
    ghapp = {
      source  = "google/gh-app-unofficial" # (Or your custom registry source once published)
      version = "~> 1.0"
    }
  }
}

provider "ghapp" {
  enterprise_slug = "my-enterprise-slug"

  # Optional: API access token (can also be set via GITHUB_TOKEN env var)
  # token           = "your-installation-access-token"

  # Optional: For GitHub Enterprise Server (on-premise) or custom API endpoints
  # (can also be set via GITHUB_BASE_URL, GITHUB_ENTERPRISE_BASE_URL, or GITHUB_API_URL env vars).
  # Defaults to https://api.github.com/
  # base_url        = "https://github.mycompany.com"
}
```

### Environment Variables

| Variable | Provider Attribute | Description |
| :--- | :--- | :--- |
| `GITHUB_TOKEN` | `token` | GitHub App Installation Access Token |
| `GITHUB_BASE_URL` / `GITHUB_ENTERPRISE_BASE_URL` / `GITHUB_API_URL` | `base_url` | GitHub Enterprise Server Base URL (e.g. `https://github.mycompany.com`) |

## Developing the Provider

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (see [Requirements](#requirements) above).

To compile the provider, run `go install`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `make generate`.

In order to run the full suite of Acceptance tests, run `make testacc`.

*Note:* Acceptance tests create real resources, and often cost money to run.

```shell
make testacc
```

## Local Development & Acceptance Testing

For full step-by-step developer onboarding, toolchain setup, dual-app GitHub App bootstrapping (`org-a` vs `org-b`), and 30-day enterprise trial rotation runbooks, see **[DEVELOPMENT.md](./DEVELOPMENT.md)**.
> [!IMPORTANT]
> **Required for Acceptance/Integration Tests:** Configuring the minimal `.env` file and the two-organization GitHub App structure described in `DEVELOPMENT.md` is **strictly required** for `make testacc` to authenticate with GitHub and execute live acceptance/integration tests successfully.

### Unified Operating Model

Both local interactive debugging (`dlv` / VS Code `F5`) and automated acceptance testing (`make testacc`) target the exact same static sandbox organization (`org-b` / `TF_VAR_target_org`) under your enterprise (`TF_VAR_enterprise_slug`) and inherit seamlessly from your minimal `.env` file:

| Category | Manual Debugging & Dev Mode (`dlv` / VS Code F5) | Automated Acceptance Testing (`make testacc` / CI) |
| :--- | :--- | :--- |
| **Purpose** | Interactive development, attaching breakpoints (`dlv`) to local HCL examples (`examples/resources/ghapp_installation`). | Automated, non-interactive CI/CD validation and regression testing against the static sandbox (`resource.Test`). |
| **Target Organization (`org-b`)** | Static dedicated organization (`TF_VAR_target_org` in `.env`). | Static dedicated organization (`TF_VAR_target_org` / `GITHUB_TARGET_ORG`). Pre-check verifies test repositories exist. |
| **App Authentication** | Uses `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PATH`, and `GITHUB_APP_INSTALLATION_ID` in `.env` to authenticate. | Uses `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PATH`, and `GITHUB_APP_INSTALLATION_ID` via `cmd/get-token` to mint `GITHUB_TOKEN`. |
| **HCL Input Variables** | Uses `TF_VAR_enterprise_slug`, `TF_VAR_target_org`, `TF_VAR_client_id`, and `TF_EXAMPLE_DIR`. | Controlled by Go test strings (`testAccConfig`), inheriting target configuration directly from `TF_VAR_*` variables. |

### Prerequisites
*   Install the [Go extension for VS Code](https://marketplace.visualstudio.com/items?itemName=golang.Go).
*   Install the Delve debugger: `go install github.com/go-delve/delve/cmd/dlv@latest`
*   Create a `.env` file in the workspace root with your GitHub App credentials and test configurations (see [DEVELOPMENT.md](./DEVELOPMENT.md) for template).
*   Create a `terraform.rc` file (or modify an existing one at `~/.terraformrc`) in the workspace root to point to your local Go bin directory (e.g. `~/go/bin`):
    ```hcl
    provider_installation {
      dev_overrides {
        "google/gh-app-unofficial" = "/path/to/your/home/go/bin"
      }
      direct {}
    }
    ```

### Running & Testing (Dev Overrides)
To test local changes with Terraform without breakpoints:
1.  Use `Ctrl+Shift+P` -> **Run Task** -> select **`Terraform Plan (Verification Example)`** or **`Terraform Apply (Verification Example)`**.
2.  This automatically builds the provider and runs Terraform using the local dev configuration.

### Debugging the Provider
To debug the provider with breakpoints (integrated VS Code workflow):
1.  In VS Code, go to the **Run and Debug** view (`Ctrl+Shift+D`).
2.  Select **`Debug Provider`** and press `F5`.
3.  Select the Terraform command you want to run (`plan`, `apply`, or `destroy`) from the dropdown prompt at the top of the window.
4.  VS Code will start the debugger in a background terminal task and automatically attach to it.
5.  Switch to the **Terminal** tab in VS Code (where the `Start Headless Debugger` task is running), and press **`ENTER`** to run Terraform. Your breakpoints in VS Code will now be hit.

### Debugging Tests
To debug unit or acceptance tests:
1.  Select **Debug Tests (Current File)** or **Debug Provider Acceptance Tests** in the Run and Debug view.
2.  Press `F5` (automatically sets `TF_ACC=1`).

### Troubleshooting: "operation not permitted" (Linux)
If the debugger fails to launch with `operation not permitted`, run:
```shell
sudo sysctl -w kernel.yama.ptrace_scope=0
```
*(Resets on reboot. To make permanent, add `kernel.yama.ptrace_scope = 0` to `/etc/sysctl.d/10-ptrace.conf`)*


