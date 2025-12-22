#!/usr/bin/env bash
#
# Backfill release labels and comments for historical releases
#
# Usage:
#   ./scripts/backfill-release-labels.sh --dry-run    # Preview changes
#   ./scripts/backfill-release-labels.sh              # Apply changes
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

# Comment templates
pr_comment() {
    local tag="$1"
    local url="https://github.com/${REPO}/releases/tag/${tag}"
    cat <<EOF
🚀 **Released in [${tag}](${url})**

Thank you for your contribution! 🙏

This is now available in the latest release. Please test and verify everything works as expected in your environment.

If you encounter any issues, please open a new issue.
EOF
}

issue_comment() {
    local tag="$1"
    local url="https://github.com/${REPO}/releases/tag/${tag}"
    cat <<EOF
🚀 **Released in [${tag}](${url})**

Thank you for reporting this! 🙏

The fix/feature is now available in the latest release. Please update and verify everything works as expected.

If the issue persists or you find related problems, please open a new issue.
EOF
}

# Check if comment already exists
has_release_comment() {
    local type="$1"  # "pr" or "issue"
    local number="$2"
    local tag="$3"

    if [[ "$type" == "pr" ]]; then
        gh pr view "$number" --repo "$REPO" --json comments --jq '.comments[].body' 2>/dev/null | grep -q "Released in \[${tag}\]" && return 0
    else
        gh issue view "$number" --repo "$REPO" --json comments --jq '.comments[].body' 2>/dev/null | grep -q "Released in \[${tag}\]" && return 0
    fi
    return 1
}

# Check if label exists
has_release_label() {
    local type="$1"
    local number="$2"
    local tag="$3"

    if [[ "$type" == "pr" ]]; then
        gh pr view "$number" --repo "$REPO" --json labels --jq '.labels[].name' 2>/dev/null | grep -q "^released:${tag}$" && return 0
    else
        gh issue view "$number" --repo "$REPO" --json labels --jq '.labels[].name' 2>/dev/null | grep -q "^released:${tag}$" && return 0
    fi
    return 1
}

# Process a single PR
process_pr() {
    local pr="$1"
    local tag="$2"
    local label="released:${tag}"

    # Check if already processed
    if has_release_label "pr" "$pr" "$tag" && has_release_comment "pr" "$pr" "$tag"; then
        log_info "PR #${pr} already processed for ${tag}, skipping"
        return 0
    fi

    if [[ "$DRY_RUN" == "--dry-run" ]]; then
        log_dry "Would label PR #${pr} with '${label}'"
        log_dry "Would comment on PR #${pr}"
    else
        # Add label
        if ! has_release_label "pr" "$pr" "$tag"; then
            gh pr edit "$pr" --repo "$REPO" --add-label "$label" 2>/dev/null && \
                log_success "Labeled PR #${pr} with '${label}'" || \
                log_warn "Failed to label PR #${pr}"
        fi

        # Add comment
        if ! has_release_comment "pr" "$pr" "$tag"; then
            pr_comment "$tag" | gh pr comment "$pr" --repo "$REPO" --body-file - 2>/dev/null && \
                log_success "Commented on PR #${pr}" || \
                log_warn "Failed to comment on PR #${pr}"
        fi
    fi
}

# Process a single issue
process_issue() {
    local issue="$1"
    local tag="$2"
    local label="released:${tag}"

    # Check if already processed
    if has_release_label "issue" "$issue" "$tag" && has_release_comment "issue" "$issue" "$tag"; then
        log_info "Issue #${issue} already processed for ${tag}, skipping"
        return 0
    fi

    if [[ "$DRY_RUN" == "--dry-run" ]]; then
        log_dry "Would label Issue #${issue} with '${label}'"
        log_dry "Would comment on Issue #${issue}"
    else
        # Add label
        if ! has_release_label "issue" "$issue" "$tag"; then
            gh issue edit "$issue" --repo "$REPO" --add-label "$label" 2>/dev/null && \
                log_success "Labeled Issue #${issue} with '${label}'" || \
                log_warn "Failed to label Issue #${issue}"
        fi

        # Add comment
        if ! has_release_comment "issue" "$issue" "$tag"; then
            issue_comment "$tag" | gh issue comment "$issue" --repo "$REPO" --body-file - 2>/dev/null && \
                log_success "Commented on Issue #${issue}" || \
                log_warn "Failed to comment on Issue #${issue}"
        fi
    fi
}

# Get PRs between two tags
get_prs_in_range() {
    local from_tag="$1"
    local to_tag="$2"

    # Get commits between tags and extract PR numbers
    gh api "repos/${REPO}/compare/${from_tag}...${to_tag}" \
        --jq '[.commits[].commit.message | capture("#(?<n>[0-9]+)"; "g") | .n] | unique | .[]' 2>/dev/null || echo ""
}

# Get linked issues for a PR
get_linked_issues() {
    local pr="$1"
    gh pr view "$pr" --repo "$REPO" --json closingIssuesReferences \
        --jq '.closingIssuesReferences[].number' 2>/dev/null || echo ""
}

# Process a release
process_release() {
    local prev_tag="$1"
    local tag="$2"

    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}Processing release: ${tag} (from ${prev_tag})${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"

    # Ensure label exists
    if [[ "$DRY_RUN" != "--dry-run" ]]; then
        gh label create "released:${tag}" --repo "$REPO" --color "0e8a16" \
            --description "Included in ${tag} release" 2>/dev/null || true
    fi

    # Get all PRs in this release
    local prs
    prs=$(get_prs_in_range "$prev_tag" "$tag")

    if [[ -z "$prs" ]]; then
        log_warn "No PRs found between ${prev_tag} and ${tag}"
        return 0
    fi

    local pr_count=0
    local issue_count=0

    for pr in $prs; do
        # Verify PR exists and is merged
        local pr_state
        pr_state=$(gh pr view "$pr" --repo "$REPO" --json state --jq '.state' 2>/dev/null || echo "")

        if [[ "$pr_state" != "MERGED" ]]; then
            log_info "PR #${pr} is not merged (state: ${pr_state:-unknown}), skipping"
            continue
        fi

        process_pr "$pr" "$tag"
        ((pr_count++)) || true

        # Process linked issues
        local issues
        issues=$(get_linked_issues "$pr")
        for issue in $issues; do
            process_issue "$issue" "$tag"
            ((issue_count++)) || true
        done
    done

    log_info "Processed ${pr_count} PRs and ${issue_count} issues for ${tag}"
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
        echo -e "${RED}║         Changes WILL be made to GitHub PRs/Issues             ║${NC}"
        echo -e "${RED}╚═══════════════════════════════════════════════════════════════╝${NC}"
        echo ""
        read -p "Are you sure you want to proceed? (yes/no): " confirm
        if [[ "$confirm" != "yes" ]]; then
            echo "Aborted."
            exit 1
        fi
    fi

    # Get all releases sorted by date (oldest first for proper processing)
    local releases
    releases=$(gh release list --repo "$REPO" --json tagName,publishedAt \
        --jq 'sort_by(.publishedAt) | .[].tagName')

    # Convert to array
    local release_array=()
    while IFS= read -r tag; do
        release_array+=("$tag")
    done <<< "$releases"

    # Process each release (skip the first one as it has no "previous")
    # Start from v0.7.0 (index where netresearch fork became active)
    local start_processing=false
    local prev_tag=""

    for tag in "${release_array[@]}"; do
        # Start processing from v0.7.0
        if [[ "$tag" == "v0.7.0" ]]; then
            start_processing=true
            prev_tag="v0.6.7"  # Use v0.6.7 as baseline for v0.7.0
        fi

        if [[ "$start_processing" == true && -n "$prev_tag" ]]; then
            process_release "$prev_tag" "$tag"
        fi

        prev_tag="$tag"
    done

    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}Backfill complete!${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
}

main "$@"
