#!/usr/bin/env bash
# Generate a seed JSON index for upstream dependency docs from any GitHub repo.
# Outputs JSON matching the format expected by `./seed features`.
#
# NOTE: The --doc-type values "upstream_release" and "upstream_issue" are not
# yet in store.EnhancementDocTypes (currently ["roadmap","adr","research"]).
# Before seeding with those types, add them to EnhancementDocTypes in
# internal/store/models.go and update the seed command validation accordingly.
# Until then, use an existing type (e.g. "research") or extend the allowed list.
#
# Usage:
#   ./scripts/generate-upstream-index.sh \
#     --repo electron/electron \
#     --type releases \
#     --version 39 \
#     --doc-type upstream_release \
#     > data/electron-v39-releases.json
#
#   ./scripts/generate-upstream-index.sh \
#     --repo electron/electron \
#     --type issues \
#     --version 39 \
#     --doc-type upstream_issue \
#     > data/electron-v39-issues.json
#
# Requires: gh (GitHub CLI), jq

set -euo pipefail

# truncate_safely trims text to at most $max chars, but never cuts mid-line.
# If the byte at position $max is in the middle of a line (i.e. not a newline
# and not past the end), we drop the trailing partial line. If the text has
# no newlines at all, we fall through to a hard slice to avoid shrinking to
# empty.
truncate_safely() {
  local text="$1"
  local max="${2:-2000}"
  if (( ${#text} <= max )); then
    printf '%s' "$text"
    return
  fi
  local cut="${text:0:$max}"
  local next_char="${text:$max:1}"
  if [[ -n "$next_char" && "$next_char" != $'\n' ]]; then
    # Drop trailing partial line if there is at least one newline to cut at.
    if [[ "$cut" == *$'\n'* ]]; then
      cut="${cut%$'\n'*}"
    fi
  fi
  printf '%s' "$cut"
}

REPO=""
TYPE=""
VERSION=""
DOC_TYPE=""

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)      REPO="$2";     shift 2 ;;
    --type)      TYPE="$2";     shift 2 ;;
    --version)   VERSION="$2";  shift 2 ;;
    --doc-type)  DOC_TYPE="$2"; shift 2 ;;
    *)
      echo "Unknown argument: $1" >&2
      echo "Usage: $0 --repo OWNER/REPO --type releases|issues --version VERSION --doc-type DOC_TYPE" >&2
      exit 1
      ;;
  esac
done

# Validate required arguments
for arg in REPO TYPE VERSION DOC_TYPE; do
  if [[ -z "${!arg}" ]]; then
    echo "Error: --$(echo "$arg" | tr '[:upper:]' '[:lower:]' | tr '_' '-') is required" >&2
    exit 1
  fi
done

if [[ "$TYPE" != "releases" && "$TYPE" != "issues" ]]; then
  echo "Error: --type must be 'releases' or 'issues'" >&2
  exit 1
fi

entries=()

if [[ "$TYPE" == "releases" ]]; then
  echo "Fetching releases from ${REPO} matching v${VERSION}.*..." >&2

  # Paginate through releases and filter by version prefix
  page=1
  while true; do
    batch=$(gh api "repos/${REPO}/releases?per_page=100&page=${page}" 2>/dev/null)
    count=$(echo "$batch" | jq 'length')
    if [[ "$count" -eq 0 ]]; then
      break
    fi

    while IFS= read -r release; do
      tag=$(echo "$release" | jq -r '.tag_name')
      body=$(echo "$release" | jq -r '.body // ""')
      published_at=$(echo "$release" | jq -r '.published_at')
      html_url=$(echo "$release" | jq -r '.html_url')

      # Truncate body to 2000 chars at last newline boundary to avoid
      # cutting mid-line (which can break markdown links).
      summary="$(truncate_safely "$body" 2000)"

      echo "  Processing release: ${tag}" >&2

      entry=$(jq -n \
        --arg topic "$tag" \
        --arg status "released" \
        --arg doc_path "releases/tag/${tag}" \
        --arg doc_url "$html_url" \
        --arg summary "$summary" \
        --arg source "$DOC_TYPE" \
        --arg last_updated "$(echo "$published_at" | cut -c1-10)" \
        '{topic: $topic, status: $status, doc_path: $doc_path, doc_url: $doc_url, summary: $summary, source: $source, last_updated: $last_updated}')
      entries+=("$entry")
    done < <(echo "$batch" | jq -c --arg prefix "v${VERSION}." '.[] | select(.tag_name | startswith($prefix))')

    page=$((page + 1))
  done

elif [[ "$TYPE" == "issues" ]]; then
  # Try milestone first, fall back to label filtering
  milestone_number=$(gh api "repos/${REPO}/milestones?per_page=100&state=all" \
    --jq ".[] | select(.title | test(\"$VERSION\")) | .number" 2>/dev/null | head -1)

  if [[ -n "$milestone_number" ]]; then
    echo "Fetching issues from ${REPO} by milestone ${milestone_number}..." >&2
    issue_query="repos/${REPO}/issues?milestone=${milestone_number}&state=all&per_page=100"
  else
    echo "No milestone found, fetching issues from ${REPO} by label '${VERSION}'..." >&2
    issue_query="repos/${REPO}/issues?labels=${VERSION}&state=all&per_page=100"
  fi

  # Paginate through issues
  page=1
  while true; do
    batch=$(gh api "${issue_query}&page=${page}" 2>/dev/null)
    count=$(echo "$batch" | jq 'length')
    if [[ "$count" -eq 0 ]]; then
      break
    fi

    while IFS= read -r issue; do
      number=$(echo "$issue" | jq -r '.number')
      title=$(echo "$issue" | jq -r '.title')
      body=$(echo "$issue" | jq -r '.body // ""')
      state=$(echo "$issue" | jq -r '.state')
      html_url=$(echo "$issue" | jq -r '.html_url')
      updated_at=$(echo "$issue" | jq -r '.updated_at')
      labels=$(echo "$issue" | jq -r '[.labels[].name] | join(", ")')

      # Build summary from title + labels + body, truncated to 2000 chars
      # at last newline boundary to avoid cutting mid-line.
      full_text="Labels: ${labels}\n\n${body}"
      summary="$(truncate_safely "$full_text" 2000)"

      echo "  Processing issue #${number}: ${title}" >&2

      entry=$(jq -n \
        --arg topic "#${number}: ${title}" \
        --arg status "$state" \
        --arg doc_path "issues/${number}" \
        --arg doc_url "$html_url" \
        --arg summary "$summary" \
        --arg source "$DOC_TYPE" \
        --arg last_updated "$(echo "$updated_at" | cut -c1-10)" \
        '{topic: $topic, status: $status, doc_path: $doc_path, doc_url: $doc_url, summary: $summary, source: $source, last_updated: $last_updated}')
      entries+=("$entry")
    done < <(echo "$batch" | jq -c '.[]')

    page=$((page + 1))
  done
fi

if [[ ${#entries[@]} -eq 0 ]]; then
  echo "Warning: no entries found for ${REPO} ${TYPE} version ${VERSION}" >&2
  echo "[]"
  exit 0
fi

# Output as JSON array
printf '%s\n' "${entries[@]}" | jq -s '.'

echo "Generated ${#entries[@]} entries" >&2
