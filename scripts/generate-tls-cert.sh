#!/bin/bash
# Generate TLS certificate with Subject Alternative Names (SANs)
# for the load balancer server

set -e

# Configuration
CERT_DIR="${1:-./certs}"
DOMAIN="${2:-lb.cyqle.local}"
DAYS="${3:-365}"

# Create certificate directory
mkdir -p "$CERT_DIR"

echo "Generating TLS certificate for: $DOMAIN"
echo "Certificate directory: $CERT_DIR"
echo "Valid for: $DAYS days"
echo ""

# Create OpenSSL configuration file with SANs
cat > "$CERT_DIR/openssl.cnf" <<EOF
[req]
default_bits = 4096
prompt = no
default_md = sha256
distinguished_name = dn
req_extensions = req_ext

[dn]
C = US
ST = State
L = City
O = Organization
CN = $DOMAIN

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = $DOMAIN
DNS.2 = localhost
IP.1 = 127.0.0.1
IP.2 = 0.0.0.0
EOF

# Generate certificate
openssl req -x509 -newkey rsa:4096 -nodes \
    -keyout "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.crt" \
    -days "$DAYS" \
    -config "$CERT_DIR/openssl.cnf" \
    -extensions req_ext

echo ""
echo "✓ Certificate generated successfully!"
echo ""
echo "Files created:"
echo "  - Certificate: $CERT_DIR/server.crt"
echo "  - Private key: $CERT_DIR/server.key"
echo ""
echo "Add these to your server config:"
echo "  tls_cert_file: $CERT_DIR/server.crt"
echo "  tls_key_file: $CERT_DIR/server.key"
echo ""
echo "For clients using self-signed certificates, add to client config:"
echo "  insecure_tls: true"
echo ""
