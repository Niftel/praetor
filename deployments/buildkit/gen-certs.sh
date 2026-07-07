#!/bin/sh
# Generates the mTLS material that secures the buildkitd gRPC listener on
# praetor-net (issue #46 hardening follow-on). One CA signs both a daemon
# (server) cert and a packbuilder (client) cert; buildkitd requires the client
# cert and the packbuilder verifies the daemon cert, so nothing else on the
# network can drive builds.
#
# Idempotent: if the CA already exists the whole step is a no-op, so restarts and
# `docker compose up` re-runs reuse the same trust root (the daemon and client
# certs stay valid). Delete the buildkit-certs volume to rotate.
set -eu

CERTS=/certs
CN_DAEMON=buildkitd     # must match BUILDKIT_TLS_SERVERNAME the client verifies
SAN="subjectAltName=DNS:buildkitd,DNS:localhost,IP:127.0.0.1"
DAYS=3650

if [ -f "$CERTS/ca.pem" ]; then
  echo "[certgen] CA already present in $CERTS — nothing to do."
  exit 0
fi

command -v openssl >/dev/null 2>&1 || apk add --no-cache openssl >/dev/null

echo "[certgen] generating CA + daemon + client certs in $CERTS ..."
umask 077

# Certificate authority.
openssl req -x509 -newkey rsa:4096 -nodes -days "$DAYS" \
  -keyout "$CERTS/ca-key.pem" -out "$CERTS/ca.pem" \
  -subj "/CN=praetor-buildkit-ca" >/dev/null 2>&1

# Daemon (server) cert — SANs so the client can verify the tcp://buildkitd name.
printf '%s\n' "$SAN" > "$CERTS/daemon-ext.cnf"
openssl req -newkey rsa:4096 -nodes \
  -keyout "$CERTS/daemon-key.pem" -out "$CERTS/daemon.csr" \
  -subj "/CN=$CN_DAEMON" >/dev/null 2>&1
openssl x509 -req -in "$CERTS/daemon.csr" \
  -CA "$CERTS/ca.pem" -CAkey "$CERTS/ca-key.pem" -CAcreateserial \
  -days "$DAYS" -extfile "$CERTS/daemon-ext.cnf" \
  -out "$CERTS/daemon-cert.pem" >/dev/null 2>&1

# Client (packbuilder) cert.
openssl req -newkey rsa:4096 -nodes \
  -keyout "$CERTS/client-key.pem" -out "$CERTS/client.csr" \
  -subj "/CN=packbuilder" >/dev/null 2>&1
openssl x509 -req -in "$CERTS/client.csr" \
  -CA "$CERTS/ca.pem" -CAkey "$CERTS/ca-key.pem" -CAcreateserial \
  -days "$DAYS" -out "$CERTS/client-cert.pem" >/dev/null 2>&1

# Both consuming containers run as root; the keys are 0600 (umask above).
rm -f "$CERTS/daemon.csr" "$CERTS/client.csr" "$CERTS/daemon-ext.cnf"
echo "[certgen] done."
