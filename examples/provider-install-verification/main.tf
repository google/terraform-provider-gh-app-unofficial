terraform {
  required_providers {
    ghapp = {
      source = "registry.terraform.io/google/gh-app-unofficial"
    }
  }
}

provider "ghapp" {}

data "ghapp_installations" "all" {
  target_org = "my-org-slug"
}
