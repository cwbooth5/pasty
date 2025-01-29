#!/usr/bin/env bash

# gen_client_cert.sh <username>
# Example usage:
#   ./gen_client_cert.sh allowedUser

USERNAME="$1"
if [ -z "$USERNAME" ]; then
  echo "Usage: $0 <username>"
  exit 1
fi

# Paths to your CA's key and cert
CA_KEY="ca_key.pem"
CA_CERT="ca_cert.pem"

# Output files for client
CLIENT_KEY="${USERNAME}_client.key"
CLIENT_CSR="${USERNAME}_client.csr"
CLIENT_CERT="${USERNAME}_client.crt"

echo "Generating client key..."
openssl genrsa -out "$CLIENT_KEY" 2048

echo "Generating CSR with Common Name = $USERNAME..."
openssl req -new -key "$CLIENT_KEY" -subj "/CN=$USERNAME" -out "$CLIENT_CSR"

echo "Signing CSR with CA..."
openssl x509 -req \
    -in "$CLIENT_CSR" \
    -CA "$CA_CERT" \
    -CAkey "$CA_KEY" \
    -CAcreateserial \
    -out "$CLIENT_CERT" \
    -days 365

echo "Generated client cert: $CLIENT_CERT"
echo "Private key: $CLIENT_KEY"
echo "Done."

