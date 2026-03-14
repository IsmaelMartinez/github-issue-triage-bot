#!/usr/bin/env bash
# Generate a feature index JSON for seeding ADR, research, and roadmap documents
# from the teams-for-linux repository into the triage bot's vector store.
#
# Usage: ./scripts/generate-feature-index.sh > data/feature-index.json
#
# Requires: gh (GitHub CLI), jq

set -euo pipefail

REPO="IsmaelMartinez/teams-for-linux"
BASE_URL="https://github.com/${REPO}/blob/main"
ADR_PATH="docs-site/docs/development/adr"
RESEARCH_PATH="docs-site/docs/development/research"
ROADMAP_PATH="docs-site/docs/development/plan/roadmap.md"

entries=()

# Fetch and process ADR documents
echo "Fetching ADR documents..." >&2
adr_files=$(gh api "repos/${REPO}/contents/${ADR_PATH}" --jq '.[] | select(.name != "README.md" and (.name | endswith(".md"))) | .name')

for file in $adr_files; do
  echo "  Processing ADR: ${file}" >&2
  content=$(gh api "repos/${REPO}/contents/${ADR_PATH}/${file}" --jq '.content' | base64 -d)
  title=$(echo "$content" | grep -m1 '^# ' | sed 's/^# //')
  # Extract status from ## Status section if present
  status=$(echo "$content" | sed -n '/^## Status/,/^## /{/^## Status/d;/^## /d;p;}' | head -5 | tr '\n' ' ' | xargs)
  if [ -z "$status" ]; then
    status="Accepted"
  fi
  # Truncate content to first 2000 chars for embedding
  summary=$(echo "$content" | head -c 2000)

  entry=$(jq -n \
    --arg topic "$title" \
    --arg status "$status" \
    --arg doc_path "${ADR_PATH}/${file}" \
    --arg doc_url "${BASE_URL}/${ADR_PATH}/${file}" \
    --arg summary "$summary" \
    --arg source "adr" \
    --arg last_updated "$(date +%Y-%m-%d)" \
    '{topic: $topic, status: $status, doc_path: $doc_path, doc_url: $doc_url, summary: $summary, source: $source, last_updated: $last_updated}')
  entries+=("$entry")
done

# Fetch and process research documents
echo "Fetching research documents..." >&2
research_files=$(gh api "repos/${REPO}/contents/${RESEARCH_PATH}" --jq '.[] | select(.name != "README.md" and (.name | endswith(".md"))) | .name')

for file in $research_files; do
  echo "  Processing research: ${file}" >&2
  content=$(gh api "repos/${REPO}/contents/${RESEARCH_PATH}/${file}" --jq '.content' | base64 -d)
  title=$(echo "$content" | grep -m1 '^# ' | sed 's/^# //')
  status="Research complete"
  summary=$(echo "$content" | head -c 2000)

  entry=$(jq -n \
    --arg topic "$title" \
    --arg status "$status" \
    --arg doc_path "${RESEARCH_PATH}/${file}" \
    --arg doc_url "${BASE_URL}/${RESEARCH_PATH}/${file}" \
    --arg summary "$summary" \
    --arg source "research" \
    --arg last_updated "$(date +%Y-%m-%d)" \
    '{topic: $topic, status: $status, doc_path: $doc_path, doc_url: $doc_url, summary: $summary, source: $source, last_updated: $last_updated}')
  entries+=("$entry")
done

# Fetch and process roadmap
echo "Fetching roadmap..." >&2
content=$(gh api "repos/${REPO}/contents/${ROADMAP_PATH}" --jq '.content' | base64 -d)
summary=$(echo "$content" | head -c 2000)

entry=$(jq -n \
  --arg topic "Development Roadmap" \
  --arg status "Active" \
  --arg doc_path "${ROADMAP_PATH}" \
  --arg doc_url "${BASE_URL}/${ROADMAP_PATH}" \
  --arg summary "$summary" \
  --arg source "roadmap" \
  --arg last_updated "$(date +%Y-%m-%d)" \
  '{topic: $topic, status: $status, doc_path: $doc_path, doc_url: $doc_url, summary: $summary, source: $source, last_updated: $last_updated}')
entries+=("$entry")

# Output as JSON array
printf '%s\n' "${entries[@]}" | jq -s '.'

echo "Generated ${#entries[@]} entries" >&2
