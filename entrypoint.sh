#!/bin/sh -l

set -e

ARGS="--token $TOKEN"

if [ "$DRY_RUN" = "true" ]; then
  ARGS="$ARGS --dry-run"
fi

if [ -n "$REPOSITORY_OWNER" ]; then
  ARGS="$ARGS --repo-owner $REPOSITORY_OWNER"
fi

if [ -n "$REPOSITORY_NAME" ]; then
  ARGS="$ARGS --repo-name $REPOSITORY_NAME"
fi

if [ -n "$PACKAGE_NAME" ]; then
  ARGS="$ARGS --package-name $PACKAGE_NAME"
fi

if [ -n "$OWNER_TYPE" ]; then
  ARGS="$ARGS --owner-type $OWNER_TYPE"
fi

if [ "$DELETE_UNTAGGED" = "false" ]; then
  ARGS="$ARGS --delete-untagged=false"
fi

if [ -n "$KEEP_AT_MOST" ]; then
  ARGS="$ARGS --keep-at-most $KEEP_AT_MOST"
fi

if [ -n "$FILTER_TAGS" ]; then
  ARGS="$ARGS --filter-tags $FILTER_TAGS"
fi

if [ -n "$SKIP_TAGS" ]; then
  ARGS="$ARGS --skip-tags $SKIP_TAGS"
fi

echo "Running with arguments: $ARGS"

exec /usr/local/bin/workflow-ghcr-cleaner $ARGS
