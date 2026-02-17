#!/usr/bin/env bash
set -euo pipefail

export GOEXPERIMENT=jsonv2
export ANTHROPIC_API_KEY=$(cat ~/.claude_key)

exec "$@"
