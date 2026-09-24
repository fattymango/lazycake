#!/bin/sh
# Bootstraps the demo gateway: registers it with the coordinator (if not
# already done - safe to rerun, `lcctl gateway create` is only called once
# per compose volume lifetime via the state file below) using the seeded
# demo customer token, then execs the real gateway binary with the
# id/token that registration produced. Real deployments do this by hand
# once with `lcctl gateway create` (task 2.7); this script exists purely
# so `docker compose up` can demo the whole path unattended.
set -eu

STATE=/var/lib/lazycake-gateway/registration
mkdir -p "$(dirname "$STATE")"

if [ ! -f "$STATE" ]; then
  echo "gateway-entrypoint: registering with coordinator..." >&2
  for i in $(seq 1 30); do
    if LAZYCAKE_COORDINATOR_ADDR="$CUSTOMER_API_ADDR" LAZYCAKE_TOKEN="$CUSTOMER_TOKEN" \
       /out/lcctl gateway create --label "$GATEWAY_LABEL" --service "$REGISTER_SERVICES" > "$STATE.tmp" 2>&1; then
      mv "$STATE.tmp" "$STATE"
      break
    fi
    sleep 2
  done
  if [ ! -f "$STATE" ]; then
    echo "gateway-entrypoint: failed to register after 30 attempts:" >&2
    cat "$STATE.tmp" >&2
    exit 1
  fi
fi

GATEWAY_ID=$(sed -n 's/^gateway_id: //p' "$STATE")
INSTALL_TOKEN=$(sed -n 's/^install_token: //p' "$STATE")

echo "gateway-entrypoint: starting gateway $GATEWAY_ID" >&2
exec env \
  LAZYCAKE_COORDINATOR_ADDR="$RELAY_ADDR" \
  LAZYCAKE_TOKEN="$INSTALL_TOKEN" \
  LAZYCAKE_GATEWAY_ID="$GATEWAY_ID" \
  LAZYCAKE_SERVICES="$GATEWAY_SERVICES" \
  LAZYCAKE_GATEWAY_KEY_PATH=/var/lib/lazycake-gateway/noise.key \
  /out/gateway
