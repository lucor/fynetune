#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

# Check all inputs before replacing any output.
for source in internal/ui/assets/logo.svg internal/ui/assets/icon.svg assets/favicon.ico assets/apple-touch-icon.png assets/social-preview.png assets/screenshot.png; do
    if [[ ! -f "$source" ]]; then
        printf 'Missing website asset: %s\n' "$source" >&2
        exit 1
    fi
done

cp internal/ui/assets/logo.svg website/fynetune-logo.svg
cp internal/ui/assets/icon.svg website/favicon.svg
cp assets/favicon.ico website/favicon.ico
cp assets/apple-touch-icon.png website/apple-touch-icon.png
cp assets/social-preview.png website/social-preview.png
cp assets/screenshot.png website/fynetune-ui.png
