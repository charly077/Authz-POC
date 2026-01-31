#!/bin/bash
# Get token for Alice
echo "Requesting token for 'alice'..."
RESPONSE=$(curl -s -X POST http://localhost:8080/realms/AuthorizationRealm/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=alice" \
  -d "password=alice" \
  -d "grant_type=password" \
  -d "client_id=envoy")

if echo "$RESPONSE" | grep -q "error"; then
    echo "Error getting token:"
    echo "$RESPONSE"
    exit 1
fi

# Extract Access Token (requires jq, or use python/grep)
if command -v jq &> /dev/null; then
    TOKEN=$(echo "$RESPONSE" | jq -r .access_token)
else
    # Fallback for no jq
    TOKEN=$(echo "$RESPONSE" | grep -o '"access_token":"[^"]*' | cut -d'"' -f4)
fi

echo ""
echo "Token obtained!"
echo "---------------------------------------------------"
echo "$TOKEN"
echo "---------------------------------------------------"
echo ""
echo "Test command:"
echo "curl -v -H \"Authorization: Bearer \$TOKEN\" http://localhost:8000/api/protected"
