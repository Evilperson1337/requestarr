#!/bin/sh
# Builds the library entry the design-sync converter bundles. Run from web/.
set -e
cd "$(dirname "$0")/.."
npx vite build --config design-sync/vite.config.ts
npx tsc -p design-sync/tsconfig.json
