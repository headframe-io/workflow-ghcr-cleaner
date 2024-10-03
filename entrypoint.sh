#!/bin/sh -l

set -e

# Initialize arguments
set -- --token "$TOKEN"

if [ "$DRY_RUN" = "true" ]; then
  set -- "$@" --dry-run
fi

if [ -n "$REPOSITORY_OWNER" ]; then
  set -- "$@" --repo-owner "$REPOSITORY_OWNER"
fi

if [ -n "$REPOSITORY_NAME" ]; then
  set -- "$@" --repo-name "$REPOSITORY_NAME"
fi

if [ -n "$PACKAGE_NAME" ]; then
  set -- "$@" --package-name "$PACKAGE_NAME"
fi

if [ -n "$OWNER_TYPE" ]; then
  set -- "$@" --owner-type "$OWNER_TYPE"
fi

if [ "$DELETE_UNTAGGED" = "false" ]; then
  set -- "$@" --delete-untagged=false
fi

if [ -n "$KEEP_AT_MOST" ]; then
  set -- "$@" --keep-at-most "$KEEP_AT_MOST"
fi

if [ -n "$FILTER_TAGS" ]; then
  set -- "$@" --filter-tags "$FILTER_TAGS"
fi

if [ -n "$SKIP_TAGS" ]; then
  set -- "$@" --skip-tags "$SKIP_TAGS"
fi

echo "Running with arguments: $*"

# Execute the Go application with the assembled arguments
exec /usr/local/bin/workflow-ghcr-cleaner "$@"
