#!/usr/bin/env bash
# Writes a PNG (2x) and an SVG next to every .excalidraw in this folder, using Excalidraw's own renderer in headless Chrome.
# First run installs the tooling into docs/diagrams/export/node_modules (gitignored) and builds a small bundle; later runs are quick.
set -euo pipefail
cd "$(dirname "$0")/export"
export PATH="$HOME/.nvm/versions/node/v24.21.0/bin:$PATH" # node is not on PATH by default on the owner's PC (see CLAUDE.md)
if [ ! -d node_modules ]; then
  npm init -y >/dev/null
  npm install --no-audit --no-fund @excalidraw/excalidraw react react-dom esbuild puppeteer-core
fi
if [ ! -f bundle.js ]; then
  npx esbuild entry.js --bundle --outfile=bundle.js --format=iife --minify --log-level=error \
    --define:process.env.NODE_ENV='"production"' --define:process.env.IS_PREACT='"false"' \
    --loader:.woff2=dataurl --loader:.woff=dataurl --loader:.ttf=dataurl --loader:.css=css --loader:.svg=dataurl --loader:.png=dataurl
fi
for f in ../*.excalidraw; do node render.mjs "$f" "${f%.excalidraw}"; done
