# Installations can be imported using the composite ID (<target_org>/<installation_id>).
#
# Because `target_org` and `client_id` are required fields in the resource schema, your `.tf` resource
# configuration block must contain matching `target_org` and `client_id` values after importing so that
# subsequent `terraform plan` runs show no unexpected diffs.
#
# Option 1: Automatic Configuration Generation (Recommended for Terraform 1.5+)
# Define an `import` block without manually declaring the `resource` block in your `.tf` file:
#
#   import {
#     to = ghapp_installation.example
#     id = "my-org/12345678"
#   }
#
# Then run the following command to let Terraform automatically generate the required configuration:
terraform plan -generate-config-out=generated.tf
#
#
# Option 2: Manual Declaration & Declarative Import (Terraform 1.5+)
# First define the resource block with the required `target_org` and `client_id` attributes:
#
#   resource "ghapp_installation" "example" {
#     target_org = "my-org"
#     client_id  = "Iv1._your_client_id"
#   }
#
#   import {
#     to = ghapp_installation.example
#     id = "my-org/12345678"
#   }
#
#
# Option 3: CLI Import
# First define the resource block with the required `target_org` and `client_id` attributes in `.tf`, then run:
terraform import ghapp_installation.example "my-org/12345678"
