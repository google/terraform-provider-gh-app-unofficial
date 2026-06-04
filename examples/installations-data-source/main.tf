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

data "ghapp_installations" "example" {
  target_org = "example-org"
}

output "installations" {
  value = data.ghapp_installations.example.installations
}