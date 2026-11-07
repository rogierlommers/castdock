#!/bin/sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ENV_FILE="$ROOT_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing $ENV_FILE. Create it with: cp .env.example .env" >&2
  exit 1
fi

set -a
. "$ENV_FILE"
set +a

cd "$ROOT_DIR"
exec go run ./cmd/castdock
