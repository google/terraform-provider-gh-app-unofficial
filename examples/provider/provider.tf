provider "ghapp" {
  enterprise_slug = "my-enterprise-slug"
  # token can also be set via the GITHUB_TOKEN environment variable
  # token           = "your-installation-access-token"

  # Optional: For GitHub Enterprise Server (on-premise) or custom API endpoints.
  # Can also be set via GITHUB_BASE_URL or GITHUB_ENTERPRISE_BASE_URL.
  # base_url        = "https://github.mycompany.com"
}

