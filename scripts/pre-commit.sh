#!/usr/bin/env bash
set -euo pipefail

echo "[pre-commit] running make fmt"
make fmt >/dev/null

# Optionally stage formatting changes so commit stays consistent
if ! git diff --quiet -- .; then
  echo "[pre-commit] staging formatting changes"
  git add -A
fi

echo "[pre-commit] running fmt-check"
make fmt-check

echo "[pre-commit] running tests"
make test

echo "[pre-commit] running lint"
make lint

echo "[pre-commit] all checks passed"

