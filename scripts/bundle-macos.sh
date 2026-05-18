#!/usr/bin/env bash
# Build the client as a macOS .app bundle with the Info.plist entries that
# unblock Screen Recording (NSScreenCaptureDescription) and local-network
# access (NSLocalNetworkUsageDescription, NSBonjourServices).
#
# Usage:
#   ./scripts/bundle-macos.sh [client|server]
#
# Output: dist/ExamMonitorStudent.app  (or ExamMonitorTeacher.app for server)
#
# Requires: Go toolchain only. Ad-hoc codesigns with the system's `codesign`
# (no developer account needed) so Gatekeeper doesn't immediately quarantine
# the build.

set -euo pipefail

target="${1:-client}"
case "$target" in
  client)
    module_dir="client"
    app_name="ExamMonitorStudent"
    bundle_id="com.exam-monitor.student"
    ;;
  server)
    module_dir="server"
    app_name="ExamMonitorTeacher"
    bundle_id="com.exam-monitor.teacher"
    ;;
  *)
    echo "usage: $0 [client|server]" >&2
    exit 1
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${VERSION:-1.0.0}"
build_number="${BUILD_NUMBER:-1}"

dist="${repo_root}/dist"
bundle="${dist}/${app_name}.app"
contents="${bundle}/Contents"
macos="${contents}/MacOS"

echo "==> cleaning ${bundle}"
rm -rf "${bundle}"
mkdir -p "${macos}"

echo "==> building Go binary"
(
  cd "${repo_root}/${module_dir}"
  go build -o "${macos}/${app_name}" .
)

echo "==> writing Info.plist"
sed \
  -e "s|__APP_NAME__|${app_name}|g" \
  -e "s|__EXECUTABLE__|${app_name}|g" \
  -e "s|__BUNDLE_ID__|${bundle_id}|g" \
  -e "s|__VERSION__|${version}|g" \
  -e "s|__BUILD_NUMBER__|${build_number}|g" \
  "${repo_root}/scripts/Info.plist.tmpl" > "${contents}/Info.plist"

echo "==> writing PkgInfo"
printf 'APPL????' > "${contents}/PkgInfo"

echo "==> stripping extended attributes (Go linker adds com.apple.provenance which codesign rejects)"
# xattr -cr alone misses some attrs in nested files on macOS 15+; iterate.
find "${bundle}" -type f -exec xattr -c {} \;
find "${bundle}" -type d -exec xattr -c {} \;

echo "==> ad-hoc codesign (so Gatekeeper doesn't quarantine the build)"
codesign --force --deep --sign - "${bundle}"

echo "==> done: ${bundle}"
echo "    Run with:  open '${bundle}'"
echo "    First launch will prompt for Screen Recording permission; grant it and relaunch."
