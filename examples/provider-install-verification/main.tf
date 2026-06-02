terraform {
  required_providers {
    gh-app = {
      source = "registry.terraform.io/google/gh-app-unofficial"
    }
  }
}

provider "gh-app" {
}

data "gh-app_installations" "example" {}
