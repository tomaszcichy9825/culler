#!/bin/zsh
# Cut a release: bump the version, land it through a PR, tag the merge.
# The tag push triggers the release workflow, which builds all three
# platforms and attaches them to a draft release with PR-title notes.
#
#   scripts/release.sh 0.3.0
#
# main is PR-only, so the bump travels as a pull request and waits for CI.
set -euo pipefail

VERSION="${1:-}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: scripts/release.sh X.Y.Z" >&2
  exit 1
fi
TAG="v$VERSION"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is not clean — commit or stash first" >&2
  exit 1
fi

git checkout main
git pull --ff-only
if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "$TAG already exists" >&2
  exit 1
fi

BRANCH="release/$TAG"
git checkout -b "$BRANCH"

perl -pi -e "s/version: \"[0-9.]+\" # The application version/version: \"$VERSION\" # The application version/" build/config.yml
perl -pi -e "s/\"file_version\": \"[0-9.]+\"/\"file_version\": \"$VERSION\"/; s/\"ProductVersion\": \"[0-9.]+\"/\"ProductVersion\": \"$VERSION\"/" build/windows/info.json
perl -pi -e "s/version=\"[0-9.]+\"/version=\"$VERSION\"/" build/windows/wails.exe.manifest

git commit -am "Release $TAG"
git push -u origin "$BRANCH"
gh pr create --title "Release $TAG" --body "Version bump for $TAG. Tag follows the merge."

echo "waiting for checks…"
gh pr checks --watch --interval 20

# Plain merge, never --auto: the tag must land on the exact merge commit.
gh pr merge --squash --delete-branch
git checkout main
git pull --ff-only

git tag -a "$TAG" -m "culler $TAG"
git push origin "$TAG"

echo
echo "done — the release workflow is building. The draft appears at:"
echo "  https://github.com/tomaszcichy9825/culler/releases"
