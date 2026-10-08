#!/bin/sh
# Release helpers for Taskfile.yml. Versions are SemVer git tags (v0.1.0); the build reads them
# with `git describe`, so a tag is the only place a version is written down.
#
#   build/release.sh check 0.2.0    can 0.2.0 be released from here? (main, clean, newer)
#   build/release.sh tag 0.2.0      create the annotated tag v0.2.0 on HEAD
#   build/release.sh docker-tags IMAGE
#                                   -t arguments for the image of the tag on HEAD: 1.2.3, 1.2, 1
#                                   (not for 0.x) and latest; a pre-release (1.3.0-rc.1) gets only
#                                   its own tag
set -eu

die() {
  echo "$*" >&2
  exit 1
}

clean() {
  [ -z "$(git status --porcelain)" ] || die "there are uncommitted changes: commit or stash them first"
}

semver() {
  echo "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' ||
    die "\"$1\" is not a SemVer version: use X.Y.Z, e.g. task release -- 0.2.0"
}

cmd=${1:-}
case "$cmd" in
check)
  v=${2:-}
  v=${v#v}
  [ -n "$v" ] || die "say which version: task release -- X.Y.Z"
  semver "$v"
  [ "$(git rev-parse --abbrev-ref HEAD)" = main ] || die "releases are made from main"
  clean
  ! git rev-parse -q --verify "refs/tags/v$v" >/dev/null || die "v$v already exists"
  last=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)
  if [ -n "$last" ]; then
    newest=$(printf '%s\n%s\n' "${last#v}" "$v" | sort -V | tail -1)
    [ "$newest" = "$v" ] || die "v$v is not newer than the last release, $last"
  fi
  ;;
tag)
  v=${2#v}
  git tag -a "v$v" -m "Game Vault v$v"
  echo "tagged v$v. Next: git push origin main v$v, then task docker:publish"
  ;;
docker-tags)
  image=${2:?image}
  tag=$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null) ||
    die "this commit has no version tag: release it first with task release -- X.Y.Z"
  clean
  v=${tag#v}
  semver "$v"
  case "$v" in
  *-*) echo "-t $image:$v" ;; # pre-release: never latest
  *)
    major=${v%%.*}
    minor=${v#*.}
    minor=${minor%%.*}
    tags="-t $image:$v -t $image:$major.$minor"
    [ "$major" = 0 ] || tags="$tags -t $image:$major"
    echo "$tags -t $image:latest"
    ;;
  esac
  ;;
*)
  die "usage: build/release.sh check|tag VERSION, or build/release.sh docker-tags IMAGE"
  ;;
esac
