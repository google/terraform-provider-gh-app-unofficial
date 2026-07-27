# Copyright 2026 Google LLC
# SPDX-License-Identifier: MPL-2.0

# Installations can be imported using the composite ID (<target_org>/<installation_id>).

#
# Because `target_org` and `client_id` are required fields in the resource schema, your `.tf` resource
# configuration block must contain matching `target_org` and `client_id` values after importing so that
# subsequent `terraform plan` runs show no unexpected diffs.
terraform import gh-app-unofficial_installation.example "my-org/12345678"
