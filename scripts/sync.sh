#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target_dir="${1:-$project_dir/../motherboardrepair-ca/docs/contact-embed}"

"$project_dir/scripts/build.sh"
mkdir -p "$target_dir"
cp -f "$project_dir/dist/contact-form.wasm" "$target_dir/contact-form.wasm"
cp -f "$project_dir/dist/wasm_exec.js" "$target_dir/wasm_exec.js"
cp -f "$project_dir/dist/loader.js" "$target_dir/loader.js"

source_digest="$(cd "$project_dir" && find cmd web -type f -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256; cd "$project_dir" && shasum -a 256 go.mod scripts/build.sh)"
source_digest="$(printf '%s\n' "$source_digest" | shasum -a 256 | awk '{print $1}')"
wasm_digest="$(shasum -a 256 "$target_dir/contact-form.wasm" | awk '{print $1}')"
runtime_digest="$(shasum -a 256 "$target_dir/wasm_exec.js" | awk '{print $1}')"
loader_digest="$(shasum -a 256 "$target_dir/loader.js" | awk '{print $1}')"
cat > "$target_dir/manifest.json" <<EOF
{
  "source_repository": "Leopere/motherboardrepair-contact",
  "source_revision": "sha256:$source_digest",
  "sha256": "$wasm_digest",
  "wasm_exec_sha256": "$runtime_digest",
  "loader_sha256": "$loader_digest",
  "go_version": "$(go version | awk '{print $3}')"
}
EOF
