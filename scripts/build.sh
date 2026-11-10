#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$project_dir/dist"
mkdir -p "$dist_dir"

GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o "$dist_dir/contact-form.wasm" "$project_dir/cmd/contact"
wasm_exec="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [[ ! -f "$wasm_exec" ]]; then
    wasm_exec="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
cp -f "$wasm_exec" "$dist_dir/wasm_exec.js"
wasm_digest="$(shasum -a 256 "$dist_dir/contact-form.wasm" | awk '{print $1}')"
runtime_digest="$(shasum -a 256 "$dist_dir/wasm_exec.js" | awk '{print $1}')"
sed -e "s/__WASM_SHA256__/$wasm_digest/g" \
    -e "s/__RUNTIME_SHA256__/$runtime_digest/g" \
    "$project_dir/web/loader.js" > "$dist_dir/loader.js"
