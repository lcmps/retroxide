#!/usr/bin/env bash
# Prints the next semantic version from Conventional Commit messages since the latest vX.Y.Z tag.
# Writes version/previous outputs and release notes when GITHUB_OUTPUT / NOTES_FILE are set.
set -euo pipefail

initial_version="${INITIAL_VERSION:-v0.1.0}"
previous="$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)"

if [[ -z "$previous" ]]; then
  next="$initial_version"
  range="HEAD"
else
  range="${previous}..HEAD"
  if [[ -z "$(git rev-list "$range")" ]]; then
    echo "no commits since $previous" >&2
    exit 1
  fi
  if [[ ! "$previous" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "latest tag $previous is not vMAJOR.MINOR.PATCH" >&2
    exit 1
  fi
  major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}"

  subjects="$(git log --format=%s "$range")"
  bodies="$(git log --format=%b "$range")"
  if grep -qE '^[a-z]+(\([^)]*\))?!:' <<<"$subjects" || grep -qE '^BREAKING[ -]CHANGE:' <<<"$bodies"; then
    next="v$((major + 1)).0.0"
  elif grep -qE '^feat(\([^)]*\))?:' <<<"$subjects"; then
    next="v${major}.$((minor + 1)).0"
  else
    next="v${major}.${minor}.$((patch + 1))"
  fi
fi

if [[ -n "${NOTES_FILE:-}" ]]; then
  {
    echo "## Changes"
    echo
    git log --no-merges --format='- %s (%h)' "$range"
    if [[ -n "$previous" ]]; then
      echo
      echo "**Full changelog:** ${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-}/compare/${previous}...${next}"
    fi
  } >"$NOTES_FILE"
fi

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "version=$next"
    echo "previous=$previous"
  } >>"$GITHUB_OUTPUT"
fi

echo "$next"
