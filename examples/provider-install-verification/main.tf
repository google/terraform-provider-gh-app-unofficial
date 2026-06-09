terraform {
  required_providers {
    ghapp = {
      source = "registry.terraform.io/google/ghapp-unofficial"
    }
  }
}

provider "ghapp" {}
