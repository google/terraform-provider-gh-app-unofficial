provider "gh-app-unofficial" {
  enterprise_slug = "my-enterprise-slug"
  # token can also be set via the GITHUB_TOKEN environment variable
  # token           = "your-installation-access-token"

  # Optional: For GitHub Enterprise Server (on-premise) or custom API endpoints.
  # Can also be set via GITHUB_BASE_URL, GITHUB_ENTERPRISE_BASE_URL, or GITHUB_API_URL environment variables.
  # base_url        = "https://github.mycompany.com"
}

