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
    username := token_payload.preferred_username
    path := http_request.path

    response := {
        "allowed": false,
        "http_status": 403,
        "body": sprintf(`
<!DOCTYPE html>
<html>
<head>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        body { font-family: 'Outfit', sans-serif; background: #0f172a; color: #e2e8f0; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0;
            background-image: radial-gradient(circle at 15%% 15%%, rgba(139,92,246,0.08) 0%%, transparent 40%%),
            radial-gradient(circle at 85%% 85%%, rgba(59,130,246,0.08) 0%%, transparent 40%%); }
        .card { background: rgba(30,41,59,0.9); padding: 2rem; border-radius: 1rem; box-shadow: 0 4px 20px rgba(0,0,0,0.4);
            max-width: 600px; width: 90%%; text-align: center; border: 1px solid #334155; backdrop-filter: blur(12px); }
        h1 { color: #f87171; margin-top: 0; font-size: 1.8rem; }
        p { color: #94a3b8; }
        .reason { background: #334155; padding: 0.75rem; border-radius: 0.5rem; color: #f1f5f9; font-family: 'Fira Code', monospace; margin: 1rem 0; font-size: 0.9rem; }
        .user-path { color: #64748b; font-size: 0.85rem; margin-bottom: 1rem; }
        .btn { display: inline-block; margin-top: 0.75rem; padding: 0.6rem 1.2rem; background: #3b82f6; color: white; text-decoration: none; border-radius: 0.5rem; font-weight: 600; font-family: 'Outfit', sans-serif; border: none; cursor: pointer; font-size: 0.9rem; transition: all 0.2s; }
        .btn:hover { box-shadow: 0 0 15px rgba(59,130,246,0.4); }
        .btn-ai { background: linear-gradient(135deg, #8b5cf6, #6d28d9); margin-right: 0.5rem; }
        .btn-ai:hover { box-shadow: 0 0 20px rgba(139,92,246,0.5); }
        .btn-ai:disabled { opacity: 0.6; cursor: not-allowed; box-shadow: none; }
        .btn-rule { background: rgba(139,92,246,0.15); color: #c4b5fd; border: 1px solid rgba(139,92,246,0.3); }
        .btn-rule:hover { background: rgba(139,92,246,0.25); }
        .btn-home { background: rgba(255,255,255,0.1); color: #e2e8f0; border: 1px solid rgba(255,255,255,0.1); }
        .buttons { display: flex; flex-wrap: wrap; justify-content: center; gap: 0.5rem; margin-top: 1.25rem; }
        .spinner { display: inline-block; width: 14px; height: 14px; border: 2px solid rgba(255,255,255,0.3); border-top-color: white; border-radius: 50%%; animation: spin 0.6s linear infinite; margin-right: 0.4rem; vertical-align: middle; }
        @keyframes spin { to { transform: rotate(360deg); } }
        #aiResult { text-align: left; margin-top: 1.25rem; padding: 1.25rem; background: rgba(0,0,0,0.3); border: 1px solid rgba(139,92,246,0.2);
            border-radius: 0.75rem; display: none; line-height: 1.7; font-size: 0.9rem; max-height: 400px; overflow-y: auto; }
        #aiResult h1, #aiResult h2, #aiResult h3, #aiResult h4 { color: #c4b5fd; margin: 0.75rem 0 0.4rem 0; }
        #aiResult h1 { font-size: 1.15rem; } #aiResult h2 { font-size: 1.05rem; } #aiResult h3 { font-size: 0.95rem; }
        #aiResult ul { padding-left: 1.5rem; margin: 0.5rem 0; }
        #aiResult li { margin-bottom: 0.25rem; }
        #aiResult code { background: rgba(139,92,246,0.15); padding: 0.1rem 0.35rem; border-radius: 3px; font-family: 'Fira Code', monospace; font-size: 0.82rem; color: #c4b5fd; }
        #aiResult pre { background: rgba(0,0,0,0.4); border: 1px solid rgba(139,92,246,0.15); border-radius: 6px; padding: 0.75rem 1rem; overflow-x: auto; margin: 0.5rem 0; }
        #aiResult pre code { background: none; padding: 0; font-size: 0.8rem; color: #e2e8f0; }
        #aiResult strong { color: #e9d5ff; }
        .ai-error { color: #fca5a5; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Access Denied</h1>
        <p>You do not have permission to access this resource.</p>
        <div class="reason">Reason: %s</div>
        <div class="user-path">User: <strong>%s</strong> &middot; Path: <strong>%s</strong></div>
        <div class="buttons">
            <button class="btn btn-ai" id="aiBtn" onclick="explainAccess()">Explain with AI</button>
            <a href="/manager" target="_blank" class="btn btn-rule">Modify Rules in AuthZ Rule Builder &rarr;</a>
            <a href="/" class="btn btn-home">Go Home</a>
        </div>
        <div id="aiResult"></div>
    </div>
    <script>
    var _u = "%s", _p = "%s", _r = "%s";
    function md(t) {
        var BT = String.fromCharCode(96);
        var codeBlocks = [];
        t = t.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");
        var cbRe = new RegExp(BT+BT+BT+"\\w*\\n([\\s\\S]*?)"+BT+BT+BT, "g");
        t = t.replace(cbRe, function(m,c) { codeBlocks.push(c.trim()); return "%%CODEBLOCK"+(codeBlocks.length-1)+"%%"; });
        var icRe = new RegExp(BT+"([^"+BT+"]+)"+BT, "g");
        t = t.replace(icRe, "<code>$1</code>");
        t = t.replace(/^#### (.+)$/gm,"<h4>$1</h4>").replace(/^### (.+)$/gm,"<h3>$1</h3>");
        t = t.replace(/^## (.+)$/gm,"<h2>$1</h2>").replace(/^# (.+)$/gm,"<h1>$1</h1>");
        t = t.replace(/\*\*(.+?)\*\*/g,"<strong>$1</strong>").replace(/\*(.+?)\*/g,"<em>$1</em>");
        t = t.replace(/^\s*[-*] (.+)$/gm,"<li>$1</li>");
        t = t.replace(/((?:<li>.*<\/li>\s*)+)/g,"<ul>$1</ul>");
        t = t.replace(/\n{2,}/g,"<br><br>").replace(/\n/g,"<br>");
        t = t.replace(/<br><br>(<h[1-4]>)/g,"$1").replace(/(<\/h[1-4]>)<br>/g,"$1");
        t = t.replace(/<br>(<ul>)/g,"$1").replace(/(<\/ul>)<br>/g,"$1");
        for (var i=0; i<codeBlocks.length; i++) {
            t = t.replace("%%CODEBLOCK"+i+"%%", "<pre><code>"+codeBlocks[i]+"</code></pre>");
        }
        t = t.replace(/<br>(<pre>)/g,"$1").replace(/(<\/pre>)<br>/g,"$1");
        return t;
    }
    function explainAccess() {
        var btn = document.getElementById("aiBtn");
        var res = document.getElementById("aiResult");
        btn.disabled = true;
        btn.innerHTML = "<span class=\"spinner\"></span>Analyzing...";
        res.style.display = "block";
        res.innerHTML = "<p style=\"color:#94a3b8\">Asking AI to explain why access was denied...</p>";
        fetch("/manager/api/explain-authz", {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({user: _u, deniedPath: _p, reason: _r, denied: true})
        }).then(function(r) { return r.json(); }).then(function(data) {
            if (data.error) { res.innerHTML = "<p class=\"ai-error\">Error: " + data.error + "</p>"; }
            else { res.innerHTML = md(data.explanation); }
        }).catch(function(e) {
            res.innerHTML = "<p class=\"ai-error\">Could not reach AI Manager: " + e.message + "</p>";
        }).finally(function() {
            btn.disabled = false;
            btn.textContent = "Explain with AI";
        });
    }
    </script>
</body>
</html>
        `, [reason, username, path, username, path, reason]),
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

http_request := input.attributes.request.http

default authorized = false

# Public paths — no auth required
is_public_path if {
    startswith(http_request.path, "/public")
}

is_public_path if {
    startswith(http_request.path, "/logout")
}

is_public_path if {
    startswith(http_request.path, "/manager")
}

is_public_path if {
    startswith(http_request.path, "/login")
}

# Home page and callback — any authenticated user can access
authorized if {
    has_valid_token
    http_request.path == "/"
}

authorized if {
    has_valid_token
    http_request.path == "/home"
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

# Protected endpoint — any authenticated user
authorized if {
    has_valid_token
    http_request.path == "/api/protected"
}

# Animals page — any authenticated user
authorized if {
    has_valid_token
    http_request.path == "/animals"
}

# Animals API — any authenticated user (OpenFGA handles per-animal access)
authorized if {
    has_valid_token
    startswith(http_request.path, "/api/animals")
}

# --- Token Handling (JWKS signature verification) ---

# Fetch JWKS from Keycloak (cached 5 min by http.send)
jwks_request := http.send({
    "url": "http://keycloak:8080/login/realms/AuthorizationRealm/protocol/openid-connect/certs",
    "method": "GET",
    "force_cache": true,
    "force_cache_duration_seconds": 300,
})

jwks := jwks_request.raw_body if {
    jwks_request.status_code == 200
}

raw_token := token if {
    auth_header := input.attributes.request.http.headers.authorization
    startswith(auth_header, "Bearer ")
    token := substring(auth_header, 7, -1)
    token != ""
}

# Verify signature against JWKS
verified_token := [valid, header, payload] if {
    [valid, header, payload] := io.jwt.decode_verify(raw_token, {"cert": jwks, "time": time.now_ns()})
}

has_valid_token if {
    verified_token[0] == true
}

token_payload := verified_token[2] if {
    verified_token[0] == true
}

# Fallback for when token is missing or invalid
token_payload := {"preferred_username": "unknown", "realm_access": {"roles": []}} if {
    not has_valid_token
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

# --- AI Generated Rules (appended by AI Manager) ---
