#!/bin/bash
# Detect container runtime
if command -v docker &> /dev/null; then
    RUNTIME="docker"
elif command -v podman &> /dev/null; then
    RUNTIME="podman"
else
    echo "Error: Neither docker nor podman found."
    exit 1
fi

echo "Using runtime: $RUNTIME"
echo "Starting Infrastructure..."
$RUNTIME compose up -d

echo "Waiting for services to be ready..."
sleep 10

echo "Infrastructure is up!"
echo "----------------------------------------"
echo "URLS:"
echo "Test App (via Envoy): http://localhost:8000"
echo "Keycloak: http://localhost:8080"
echo "OpenFGA: http://localhost:3000 (Playground)"
echo "AI Manager: http://localhost:5001"
echo "----------------------------------------"
echo "To configure Keycloak:"
echo "1. Login to http://localhost:8080 (admin/admin)"
echo "2. Create a Realm 'AuthorizationRealm'"
echo "3. Create a Client 'envoy' (OpenID Connect, Client Authentication: On, Service Accounts Enabled: On)"
echo "4. Create User 'alice' and 'bob'"
echo "----------------------------------------"
echo "AI Manager requires GEMINI_API_KEY in .env or passed to the container."
