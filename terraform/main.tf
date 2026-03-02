# Google Cloud infrastructure for the GitHub Issue Triage Bot.
#
# This manages: Artifact Registry, Cloud Run service, budget alerts.
# Neon PostgreSQL is managed separately (no official Terraform provider).
# The GCP project itself is assumed to exist and be configured externally.

terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

provider "google" {
  project = var.gcp_project_id
  region  = var.gcp_region
}

# Billing budgets API requires a quota project for user credentials.
provider "google" {
  alias                 = "billing"
  project               = var.gcp_project_id
  region                = var.gcp_region
  user_project_override = true
  billing_project       = var.gcp_project_id
}

# --- Variables ---

variable "gcp_project_id" {
  description = "GCP project ID"
  type        = string
  default     = "gen-lang-client-0421325030"
}

variable "gcp_region" {
  description = "GCP region for Cloud Run and Artifact Registry"
  type        = string
  default     = "us-central1"
}

variable "gcp_project_number" {
  description = "GCP project number (numeric)"
  type        = string
  default     = "62054333602"
}

variable "billing_account_id" {
  description = "GCP billing account ID"
  type        = string
  default     = "01B3C7-DE2DE2-BB9ACE"
}

variable "database_url" {
  description = "Neon PostgreSQL pooler connection string"
  type        = string
  sensitive   = true
}

variable "gemini_api_key" {
  description = "Gemini API key for LLM and embeddings"
  type        = string
  sensitive   = true
}

variable "github_token" {
  description = "GitHub token for posting comments"
  type        = string
  sensitive   = true
}

variable "webhook_secret" {
  description = "Shared secret for GitHub webhook signature verification"
  type        = string
  sensitive   = true
}

variable "image_tag" {
  description = "Docker image tag to deploy"
  type        = string
  default     = "v1"
}

# --- APIs ---

resource "google_project_service" "run" {
  service            = "run.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "artifactregistry" {
  service            = "artifactregistry.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "billingbudgets" {
  service            = "billingbudgets.googleapis.com"
  disable_on_destroy = false
}

# --- Artifact Registry ---

resource "google_artifact_registry_repository" "triage_bot" {
  location      = var.gcp_region
  repository_id = "triage-bot"
  format        = "DOCKER"
  description   = "Issue triage bot Docker images"

  depends_on = [google_project_service.artifactregistry]
}

# --- Cloud Run ---

resource "google_cloud_run_v2_service" "triage_bot" {
  name     = "triage-bot"
  location = var.gcp_region
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    containers {
      image = "${var.gcp_region}-docker.pkg.dev/${var.gcp_project_id}/triage-bot/server:${var.image_tag}"

      ports {
        container_port = 8080
      }

      env {
        name  = "DATABASE_URL"
        value = var.database_url
      }
      env {
        name  = "GEMINI_API_KEY"
        value = var.gemini_api_key
      }
      env {
        name  = "GITHUB_TOKEN"
        value = var.github_token
      }
      env {
        name  = "WEBHOOK_SECRET"
        value = var.webhook_secret
      }

      resources {
        cpu_idle = true
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
      }
    }

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    timeout = "30s"
  }

  depends_on = [google_project_service.run]
}

# Allow unauthenticated access (GitHub webhooks)
resource "google_cloud_run_v2_service_iam_member" "public" {
  project  = var.gcp_project_id
  location = var.gcp_region
  name     = google_cloud_run_v2_service.triage_bot.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# --- Budget ---

resource "google_billing_budget" "triage_bot" {
  provider        = google.billing
  billing_account = var.billing_account_id
  display_name    = "Triage Bot Budget"

  budget_filter {
    projects               = ["projects/${var.gcp_project_number}"]
    credit_types_treatment = "INCLUDE_ALL_CREDITS"
    calendar_period        = "MONTH"
  }

  amount {
    specified_amount {
      currency_code = "GBP"
      units         = "15"
    }
  }

  # Alert at ~£0.75 (5%)
  threshold_rules {
    threshold_percent = 0.05
    spend_basis       = "CURRENT_SPEND"
  }

  # Alert at ~£3.75 (25%)
  threshold_rules {
    threshold_percent = 0.25
    spend_basis       = "CURRENT_SPEND"
  }

  # Alert at ~£7.50 (50%)
  threshold_rules {
    threshold_percent = 0.50
    spend_basis       = "CURRENT_SPEND"
  }

  depends_on = [google_project_service.billingbudgets]
}

# --- Outputs ---

output "cloud_run_url" {
  description = "Cloud Run service URL for webhook configuration"
  value       = google_cloud_run_v2_service.triage_bot.uri
}

output "artifact_registry_url" {
  description = "Artifact Registry repository URL"
  value       = "${var.gcp_region}-docker.pkg.dev/${var.gcp_project_id}/triage-bot"
}
