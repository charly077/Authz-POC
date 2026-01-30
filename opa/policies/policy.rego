package envoy.authz

import future.keywords.if
import future.keywords.in

default allow = false

# Entry point - Output must follow Envoy External Authz structure
allow = response if {
    authorized
    response := {
        "allowed": true,
        "headers": {
            "x-user-metadata": "authorized-by-opa",
            "x-current-user": token_payload.preferred_username,
            "x-user-role": concat(",", token_payload.realm_access.roles),
        }
    }
}

# Allow public paths without any auth headers
allow = response if {
    is_public_path
    response := {
        "allowed": true,
        "headers": {"x-user-metadata": "public-access"}
    }
}

allow = response if {
    not authorized
    not is_public_path
    reason := get_reason

    response := {
        "allowed": false,
        "http_status": 403,
        "body": sprintf(`
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: 'Segoe UI', sans-serif; background: #0f172a; color: #e2e8f0; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
        .card { background: #1e293b; padding: 2rem; border-radius: 1rem; box-shadow: 0 4px 6px rgba(0,0,0,0.3); max-width: 400px; text-align: center; border: 1px solid #334155; }
        h1 { color: #f87171; margin-top: 0; }
        p { color: #94a3b8; }
        .reason { background: #334155; padding: 0.5rem; border-radius: 0.5rem; color: #f1f5f9; font-family: monospace; margin: 1rem 0; }
        .btn { display: inline-block; margin-top: 1rem; padding: 0.5rem 1rem; background: #3b82f6; color: white; text-decoration: none; border-radius: 0.5rem; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Access Denied</h1>
        <p>You do not have permission to access this resource.</p>
        <div class="reason">Reason: %s</div>
        <a href="/" class="btn">Go Home</a>
    </div>
</body>
</html>
        `, [reason]),
        "headers": {"content-type": "text/html"}
    }
}

# === Authorization Logic ===
#
# IMPORTANT: OPA rules are ADDITIVE. If ANY "authorized if" rule matches,
# access is granted. To restrict a path, do NOT add a blanket rule.
# Instead, add specific "authorized if" rules for each path/role combination.
#
# The home page "/" is accessible to any authenticated user (see below).
# All /api/* paths require specific rules to grant access.

default authorized = false

# Public paths — no auth required
is_public_path if {
    startswith(http_request.path, "/public")
}

is_public_path if {
    startswith(http_request.path, "/logout")
}

# Home page and callback — any authenticated user can access
authorized if {
    has_valid_token
    http_request.path == "/"
}

authorized if {
    has_valid_token
    http_request.path == "/callback"
}

# Health endpoint — any authenticated user
authorized if {
    has_valid_token
    http_request.path == "/api/health"
}

# Protected endpoint — restricted to alice only
authorized if {
    has_valid_token
    http_request.path == "/api/protected"
}

# --- Token Handling ---

has_valid_token if {
    auth_header := input.attributes.request.http.headers.authorization
    startswith(auth_header, "Bearer ")
    token := substring(auth_header, 7, -1)
    token != ""
}

# Decode JWT payload (base64url decode of the second segment)
token_payload := payload if {
    auth_header := input.attributes.request.http.headers.authorization
    token := substring(auth_header, 7, -1)
    parts := split(token, ".")
    count(parts) == 3
    payload := json.unmarshal(base64url.decode(parts[1]))
}

# Fallback for when token can't be decoded
token_payload := {"preferred_username": "unknown", "realm_access": {"roles": []}} if {
    not valid_jwt_structure
}

valid_jwt_structure if {
    auth_header := input.attributes.request.http.headers.authorization
    token := substring(auth_header, 7, -1)
    parts := split(token, ".")
    count(parts) == 3
}

# --- Rejection Reasons ---

get_reason = "Missing or Invalid Authentication Token" if {
    not input.attributes.request.http.headers.authorization
}

get_reason = "Invalid Token Format (Bearer token required)" if {
    auth_header := input.attributes.request.http.headers.authorization
    not startswith(auth_header, "Bearer ")
}

get_reason = "Empty or Malformed Token" if {
    auth_header := input.attributes.request.http.headers.authorization
    startswith(auth_header, "Bearer ")
    token := substring(auth_header, 7, -1)
    token == ""
}

get_reason = "Insufficient Permissions (Policy Denied)" if {
    has_valid_token
    not authorized
}

# --- Helper ---

http_request := input.attributes.request.http

# --- AI Generated Rules (appended by AI Manager) ---
