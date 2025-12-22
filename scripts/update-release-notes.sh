#!/usr/bin/env bash
#
# Update release notes with links to PRs/Issues included in each release
#
# Usage:
#   ./scripts/update-release-notes.sh --dry-run    # Preview changes
#   ./scripts/update-release-notes.sh              # Apply changes
#
set -euo pipefail

DRY_RUN="${1:-}"
REPO="netresearch/ofelia"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_dry() { echo -e "${YELLOW}[DRY-RUN]${NC} $1"; }

# Update release notes for a single release
update_release_notes() {
    local tag="$1"
    local label="released:${tag}"

    log_info "Processing release notes for ${tag}"

    # Get existing release notes
    local notes
    notes=$(gh release view "$tag" --repo "$REPO" --json body --jq '.body' 2>/dev/null || echo "")

    if [[ -z "$notes" ]]; then
        log_warn "No release notes found for ${tag}, skipping"
        return 0
    fi

    # Check if "Included in this release" section already exists
    if echo "$notes" | grep -q "## Included in this release"; then
        log_info "PR/Issue links section already exists for ${tag}, skipping"
        return 0
    fi

    # Get PRs with this release label
    local prs
    prs=$(gh pr list --state merged --label "$label" --repo "$REPO" \
        --json number,title \
        --jq '.[] | "- [#\(.number)](https://github.com/'"$REPO"'/pull/\(.number)) \(.title)"' 2>/dev/null || echo "")

    # Get Issues with this release label
    local issues
    issues=$(gh issue list --state closed --label "$label" --repo "$REPO" \
        --json number,title \
        --jq '.[] | "- [#\(.number)](https://github.com/'"$REPO"'/issues/\(.number)) \(.title)"' 2>/dev/null || echo "")

    # Skip if no PRs or issues found
    if [[ -z "$prs" && -z "$issues" ]]; then
        log_warn "No PRs or Issues found with label ${label}, skipping"
        return 0
    fi

    # Build the new section
    local new_section=""
    new_section+="\n---\n\n## Included in this release\n\n"

    if [[ -n "$prs" ]]; then
        new_section+="### Pull Requests\n\n${prs}\n\n"
    fi

    if [[ -n "$issues" ]]; then
        new_section+="### Issues\n\n${issues}\n\n"
    fi

    if [[ "$DRY_RUN" == "--dry-run" ]]; then
        log_dry "Would update release notes for ${tag}:"
        echo -e "$new_section"
    else
        # Write updated notes to temp file
        echo -e "${notes}${new_section}" > /tmp/release_notes.md
        gh release edit "$tag" --repo "$REPO" --notes-file /tmp/release_notes.md
        log_success "Updated release notes for ${tag}"
    fi
}

# Main
main() {
    if [[ "$DRY_RUN" == "--dry-run" ]]; then
        echo -e "${YELLOW}╔═══════════════════════════════════════════════════════════════╗${NC}"
        echo -e "${YELLOW}║                      DRY RUN MODE                             ║${NC}"
        echo -e "${YELLOW}║           No changes will be made to GitHub                   ║${NC}"
        echo -e "${YELLOW}╚═══════════════════════════════════════════════════════════════╝${NC}"
    else
        echo -e "${RED}╔═══════════════════════════════════════════════════════════════╗${NC}"
        echo -e "${RED}║                      LIVE MODE                                ║${NC}"
        echo -e "${RED}║         Changes WILL be made to GitHub releases               ║${NC}"
        echo -e "${RED}╚═══════════════════════════════════════════════════════════════╝${NC}"
        echo ""
        read -p "Are you sure you want to proceed? (yes/no): " confirm
        if [[ "$confirm" != "yes" ]]; then
            echo "Aborted."
            exit 1
        fi
    fi

    # Get all releases sorted by date (oldest first)
    local releases
    releases=$(gh release list --repo "$REPO" --json tagName,publishedAt \
        --jq 'sort_by(.publishedAt) | .[].tagName')

    # Process each release starting from v0.7.0
    local start_processing=false

    for tag in $releases; do
        # Start processing from v0.7.0
        if [[ "$tag" == "v0.7.0" ]]; then
            start_processing=true
        fi

        if [[ "$start_processing" == true ]]; then
            update_release_notes "$tag"
        fi
    done

    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}Release notes update complete!${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
}

main "$@"
