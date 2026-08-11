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

# --no-tags: draft releases park untagged-* refs on the remote; they are
# GitHub's business, not this clone's.
git checkout main
git pull --ff-only --no-tags
if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "$TAG already exists" >&2
  exit 1
fi

BRANCH="release/$TAG"
git checkout -b "$BRANCH"

perl -pi -e "s/version: \"[0-9.]+\" # The application version/version: \"$VERSION\" # The application version/" build/config.yml
perl -pi -e "s/\"file_version\": \"[0-9.]+\"/\"file_version\": \"$VERSION\"/; s/\"ProductVersion\": \"[0-9.]+\"/\"ProductVersion\": \"$VERSION\"/" build/windows/info.json
# Only the application's own assemblyIdentity carries the app version. Two
# other version attributes live in this file and neither is ours: the XML
# declaration's, which stays 1.0, and the Microsoft.Windows.Common-Controls
# dependency's, which is the fixed 6.0.0.0 that names the v6 common controls
# assembly. Matching on assemblyIdentity alone rewrote that one too — every
# release from v0.3.0 to v0.5.0 shipped a Windows binary asking for a
# Common-Controls version that has never existed — so the line is picked by
# the bundle identity instead.
perl -pi -e "if (/name=\"com\\.tomaszcichy9825\\.culler\"/) { s/(assemblyIdentity[^>]*version=)\"[0-9.]+\"/\${1}\"$VERSION\"/ }" build/windows/wails.exe.manifest

git commit -am "Release $TAG"
git push -u origin "$BRANCH"
gh pr create --title "Release $TAG" --body "Version bump for $TAG. Tag follows the merge."

# CI takes a moment to register its checks on a fresh PR; watching too early
# reports "no checks" and dies. Wait for them to exist, then watch — a real
# check failure still fails the script.
echo "waiting for checks…"
for _ in $(seq 1 30); do
  count=$(gh pr checks --json state --jq 'length' 2>/dev/null || echo 0)
  [ "$count" -gt 0 ] && break
  sleep 10
done
gh pr checks --watch --interval 20

# Plain merge, never --auto: the tag must land on the exact merge commit.
gh pr merge --squash --delete-branch
git checkout main
git pull --ff-only --no-tags

git tag -a "$TAG" -m "culler $TAG"
git push origin "$TAG"

echo
echo "done — the release workflow is building. The draft appears at:"
echo "  https://github.com/tomaszcichy9825/culler/releases"
