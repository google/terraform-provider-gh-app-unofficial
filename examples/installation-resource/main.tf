terraform {
  required_providers {
    ghapp = {
      source = "registry.terraform.io/google/gh-app-unofficial"
    }
  }
}

provider "ghapp" {
  enterprise_slug = "example-enterprise"
}

resource "ghapp_installation" "test-app" {
  target_org = "example-org"
  client_id  = "Iv1.0123456789abcdef"
}

output "installation" {
  value = ghapp_installation.test-app
}
