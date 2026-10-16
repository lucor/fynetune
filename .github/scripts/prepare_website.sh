#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

# Check all inputs before replacing any output.
for source in internal/ui/assets/logo.svg internal/ui/assets/icon.svg assets/screenshot.png; do
    if [[ ! -f "$source" ]]; then
        printf 'Missing website asset: %s\n' "$source" >&2
        exit 1
    fi
done

cp internal/ui/assets/logo.svg website/fynetune-logo.svg
cp internal/ui/assets/icon.svg website/favicon.svg
cp assets/screenshot.png website/fynetune-ui.png
