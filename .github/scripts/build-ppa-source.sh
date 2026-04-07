#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  echo "usage: $0 <suite> <upstream-version> [ppa-revision]" >&2
  exit 1
fi

suite="$1"
upstream_version="${2#v}"
ppa_revision="${3:-1}"

case "$suite" in
  noble)
    ;;
  plucky)
    ;;
  *)
    echo "unsupported suite: $suite" >&2
    exit 1
    ;;
esac

repo_root="$(pwd)"
package_name="timertab"
package_version="${upstream_version}-0ppa${ppa_revision}~${suite}1"
artifact_dir="${repo_root}/dist/ppa/${suite}"
build_root="$(mktemp -d)"
tree_name="${package_name}-${upstream_version}"
tree_dir="${build_root}/${tree_name}"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(date +%s)}"
changelog_date="$(LC_ALL=C.UTF-8 date -R)"

cleanup() {
  rm -rf "${build_root}"
}
trap cleanup EXIT

mkdir -p "${artifact_dir}" "${tree_dir}"

tar \
  --exclude='./.git' \
  --exclude='./.idea' \
  --exclude='./.vscode' \
  --exclude='./.claude' \
  --exclude='./.codex' \
  --exclude='./z_local' \
  --exclude='./bin' \
  --exclude='./dist' \
  --exclude='./timertab' \
  --exclude='./vendor' \
  --exclude='./debian/.build' \
  --exclude='./debian/files' \
  --exclude='./debian/*.debhelper*' \
  --exclude='./debian/*.substvars' \
  -cf - . | tar -C "${tree_dir}" -xf -

(
  cd "${tree_dir}"
  go mod vendor
)

cat > "${tree_dir}/debian/changelog" <<EOF
timertab (${package_version}) ${suite}; urgency=medium

  * Publish ${upstream_version} for Ubuntu ${suite}.
  * Vendor Go modules in the source package for offline Launchpad builds.

 -- Michal Wadas <michalwadas@gmail.com>  ${changelog_date}
EOF

tar \
  --sort=name \
  --mtime="@${source_date_epoch}" \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  --exclude="${tree_name}/debian" \
  -C "${build_root}" \
  -cJf "${build_root}/${package_name}_${upstream_version}.orig.tar.xz" \
  "${tree_name}"

build_args=(-S -sa -d)
if [ -n "${DEBSIGN_KEYID:-}" ]; then
  build_args+=("-k${DEBSIGN_KEYID}")
else
  build_args+=(-us -uc)
fi

(
  cd "${tree_dir}"
  dpkg-buildpackage "${build_args[@]}"
)

mv "${build_root}/${package_name}_${upstream_version}.orig.tar.xz" "${artifact_dir}/"
mv "${build_root}/${package_name}_${package_version}.debian.tar.xz" "${artifact_dir}/"
mv "${build_root}/${package_name}_${package_version}.dsc" "${artifact_dir}/"
mv "${build_root}/${package_name}_${package_version}_source.buildinfo" "${artifact_dir}/"
mv "${build_root}/${package_name}_${package_version}_source.changes" "${artifact_dir}/"

printf '%s\n' "${artifact_dir}/${package_name}_${package_version}_source.changes"
