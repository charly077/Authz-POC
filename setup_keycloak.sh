#!/bin/bash
set -e

# Login
/opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin

# Create Realm
echo "Creating Realm 'myrealm'..."
if ! /opt/keycloak/bin/kcadm.sh get realms/myrealm &> /dev/null; then
    /opt/keycloak/bin/kcadm.sh create realms -s realm=myrealm -s enabled=true
fi

# Create Client 'envoy'
echo "Creating Client 'envoy'..."
if ! /opt/keycloak/bin/kcadm.sh get clients -r myrealm -q clientId=envoy | grep "envoy" &> /dev/null; then
    /opt/keycloak/bin/kcadm.sh create clients -r myrealm \
        -s clientId=envoy \
        -s protocol=openid-connect \
        -s enabled=true \
        -s publicClient=false \
        -s clientAuthenticatorType=client-secret \
        -s secret=envoy-secret \
        -s "redirectUris=[\"*\"]" \
        -s directAccessGrantsEnabled=true \
        -s serviceAccountsEnabled=true \
        -s standardFlowEnabled=true \
        -s webOrigins='["*"]'
else
    # Update existing to ensure secret matches
    ID=$(/opt/keycloak/bin/kcadm.sh get clients -r myrealm -q clientId=envoy --fields id --format csv --noquotes)
    /opt/keycloak/bin/kcadm.sh update clients/$ID -r myrealm -s secret=envoy-secret -s "redirectUris=[\"*\"]" -s publicClient=false
fi

# Create Users
for USER in alice bob; do
    echo "Creating User $USER..."
    if ! /opt/keycloak/bin/kcadm.sh get users -r myrealm -q username=$USER | grep "$USER" &> /dev/null; then
        /opt/keycloak/bin/kcadm.sh create users -r myrealm -s username=$USER -s enabled=true
        /opt/keycloak/bin/kcadm.sh set-password -r myrealm --username $USER --new-password $USER
    fi
done

echo "Keycloak Configuration Complete!"
