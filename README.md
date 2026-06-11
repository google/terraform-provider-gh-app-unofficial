# Terraform Provider Scaffolding (Terraform Plugin Framework)

_This template repository is built on the [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework). The template repository built on the [Terraform Plugin SDK](https://github.com/hashicorp/terraform-plugin-sdk) can be found at [terraform-provider-scaffolding](https://github.com/hashicorp/terraform-provider-scaffolding). See [Which SDK Should I Use?](https://developer.hashicorp.com/terraform/plugin/framework-benefits) in the Terraform documentation for additional information._

This repository is a *template* for a [Terraform](https://www.terraform.io) provider. It is intended as a starting point for creating Terraform providers, containing:

- A resource and a data source (`internal/provider/`),
- Examples (`examples/`) and generated documentation (`docs/`),
- Miscellaneous meta files.

These files contain boilerplate code that you will need to edit to create your own Terraform provider. Tutorials for creating Terraform providers can be found on the [HashiCorp Developer](https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework) platform. _Terraform Plugin Framework specific guides are titled accordingly._

Please see the [GitHub template repository documentation](https://help.github.com/en/github/creating-cloning-and-archiving-repositories/creating-a-repository-from-a-template) for how to create a new repository from this template on GitHub.

Once you've written your provider, you'll want to [publish it on the Terraform Registry](https://developer.hashicorp.com/terraform/registry/providers/publishing) so that others can use it.

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

Fill this in for each provider

## Developing the Provider

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (see [Requirements](#requirements) above).

To compile the provider, run `go install`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `make generate`.

In order to run the full suite of Acceptance tests, run `make testacc`.

*Note:* Acceptance tests create real resources, and often cost money to run.

```shell
make testacc
```

## Local Development & Debugging

### Prerequisites
*   Install the [Go extension for VS Code](https://marketplace.visualstudio.com/items?itemName=golang.Go).
*   Install the Delve debugger: `go install github.com/go-delve/delve/cmd/dlv@latest`
*   Create a `.env` file in the workspace root with your GitHub App credentials and test configurations:
    ```env
    GITHUB_APP_ID="YOUR_GITHUB_APP_ID"
    GITHUB_APP_PRIVATE_KEY_PATH="~/path/to/your/private-key.pem"
    GITHUB_APP_INSTALLATION_ID="YOUR_GITHUB_APP_INSTALLATION_ID"

    # (Optional) Target example folder to run/debug. Defaults to examples/provider-install-verification
    TF_EXAMPLE_DIR="examples/installations-data-source"
    ```
    *(Note: You can either create your own GitHub App for testing or obtain credentials/keys for a shared test App from the repository owner).*
*   Create a `terraform.rc` file in the workspace root to point to your local Go bin directory (e.g. `~/go/bin`):
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
2.  This automatically builds the provider and runs Terraform using the local `terraform.rc` configuration.

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


