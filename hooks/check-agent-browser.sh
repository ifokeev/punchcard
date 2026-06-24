#!/usr/bin/env bash
# punchcard SessionStart hook — non-invasive: it only REMINDS, never installs.
# agent-browser powers richer web proof-of-work; tasks still run without it (the
# Engineer skill falls back to a screenshot / test-output as proof).
command -v agent-browser >/dev/null 2>&1 || \
  echo "punchcard: 'agent-browser' not found — install it (see https://github.com/vercel-labs/agent-browser) to enable web proof-of-work. Tasks still work without it."
exit 0
