#!/bin/sh
# Entrypoint of the dev image (make up). Go reads SSL_CERT_FILE as the whole trust store, so the Clerk twin's
# self-signed certificate is appended to the system roots (real provider upstreams keep verifying).
set -eu
if [ -s /ca/ca.pem ]; then
  cat /etc/ssl/certs/ca-certificates.crt /ca/ca.pem > /tmp/roots.pem
  export SSL_CERT_FILE=/tmp/roots.pem
fi
exec /app/meterwised "$@"
