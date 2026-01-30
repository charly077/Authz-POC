const express = require('express');
const bodyParser = require('body-parser');
const { GoogleGenerativeAI } = require('@google/generative-ai');
const fs = require('fs');
const path = require('path');
const axios = require('axios');

const app = express();
const port = 5000;

app.use(bodyParser.json());
app.use(express.static('public'));

const genAI = new GoogleGenerativeAI(process.env.GEMINI_API_KEY || "YOUR_API_KEY");

const POLICY_PATH = '/policies/policy.rego';

// Read the current OPA policy from disk
function readCurrentPolicy() {
    try {
        return fs.readFileSync(POLICY_PATH, 'utf8');
    } catch (err) {
        return null;
    }
}

// Build the system prompt for rule generation with full OPA context
function buildGeneratePrompt(currentPolicy) {
    return `You are an expert in OPA Rego policy writing for an Envoy external authorization system.

=== ARCHITECTURE ===
- Envoy proxy forwards every HTTP request to OPA via gRPC ext_authz.
- OPA evaluates the policy and returns: allowed (bool), headers (map), http_status, body.
- Keycloak issues JWT tokens (OIDC). The token is in the Authorization header as "Bearer <jwt>".
- The policy runs under package "envoy.authz". You MUST NOT create a new package.
- The input structure is: input.attributes.request.http.{method, path, headers, host, ...}
- JWT claims are decoded by the existing token_payload rule (base64url decode of JWT).
- token_payload contains: preferred_username, realm_access.roles[], email, etc.

=== CRITICAL: HOW OPA AUTHORIZATION WORKS ===
OPA rules are ADDITIVE. If ANY "authorized if" rule evaluates to true, access is granted.
This means you CANNOT restrict access by adding a new rule — you can only GRANT access.

The policy uses PER-PATH authorization. Each path has its own "authorized if" rule:
- "/" → any authenticated user
- "/callback" → any authenticated user
- "/api/health" → any authenticated user
- "/api/protected" → has its own rule (see current policy)

To RESTRICT a path (e.g., "only alice can access /api/protected"), you must REPLACE
the existing rule for that path with a more restrictive one. Set "replaces" in your
output to the exact rule text that should be replaced.

To GRANT access to a NEW path, just add a new "authorized if" rule.

=== CURRENT POLICY (policy.rego) ===
\`\`\`rego
${currentPolicy}
\`\`\`

=== EXISTING RULES YOU CAN USE ===
- http_request: shortcut for input.attributes.request.http
- http_request.path: the request path (e.g., "/api/protected")
- http_request.method: HTTP method (e.g., "GET", "POST")
- http_request.headers: all request headers
- token_payload: decoded JWT payload with user info
- token_payload.preferred_username: the username (e.g., "alice")
- token_payload.realm_access.roles: array of role strings (e.g., ["user", "admin"])
- has_valid_token: true if a valid Bearer token is present
- is_public_path: true if the path starts with "/public" or "/logout"
- authorized: the main authorization decision (true/false)

=== WHAT YOU CAN GENERATE ===
You must ONLY generate one of these types of rules:

1. **"authorized if" rules** - conditions that grant access for a path. Example:
   authorized if {
       has_valid_token
       token_payload.preferred_username == "alice"
       http_request.path == "/api/protected"
   }

2. **"is_public_path if" rules** - to make paths publicly accessible. Example:
   is_public_path if {
       startswith(http_request.path, "/health")
   }

3. **Helper rules** - intermediate rules used by authorized. Example:
   is_admin if {
       some role in token_payload.realm_access.roles
       role == "admin"
   }

=== RESTRICTING ACCESS (IMPORTANT) ===
To restrict an EXISTING path, you must REPLACE the current rule.
Example: to restrict /api/protected to only alice:

The current rule is:
  authorized if {
      has_valid_token
      http_request.path == "/api/protected"
  }

You must set "replaces" to the EXACT current rule text, and "code" to the new rule:
  "replaces": "# Protected endpoint — default: any authenticated user can access\\n# (AI-generated rules below can override/restrict this)\\nauthorized if {\\n    has_valid_token\\n    http_request.path == \\"/api/protected\\"\\n}"
  "code": "# Protected endpoint — restricted to alice only\\nauthorized if {\\n    has_valid_token\\n    token_payload.preferred_username == \\"alice\\"\\n    http_request.path == \\"/api/protected\\"\\n}"

=== STRICT RULES - VIOLATIONS WILL BE REJECTED ===
- NEVER output "package" declarations
- NEVER output "import" statements
- NEVER output "default allow" or "default authorized"
- NEVER redefine "allow = response if" (the allow/response logic is fixed)
- NEVER use "input.user" or "input.path" or "input.method" — these DON'T EXIST
- NEVER generate standalone "allow { ... }" rules
- Use "if" keyword syntax (already imported): write "authorized if {" not "authorized {"
- Generated code will either REPLACE an existing rule or be APPENDED to the policy.

=== OUTPUT FORMAT ===
Return ONLY a JSON object (no markdown fences):
{
  "type": "OPA",
  "code": "<new rego rule(s)>",
  "replaces": "<exact text of existing rule to replace, or empty string if appending>",
  "explanation": "<brief explanation of what the rule does>"
}

If the request is better suited for OpenFGA (relationship-based, e.g., "user X is owner of document Y"), return:
{
  "type": "OpenFGA",
  "code": "<OpenFGA DSL model>",
  "replaces": "",
  "explanation": "<brief explanation>"
}`;
}

app.post('/api/generate-rule', async (req, res) => {
    const { prompt } = req.body;
    if (!prompt) {
        return res.status(400).json({ error: "No prompt provided" });
    }
    try {
        const model = genAI.getGenerativeModel({ model: "gemini-2.0-flash" });
        const currentPolicy = readCurrentPolicy() || "Policy not available";
        const systemPrompt = buildGeneratePrompt(currentPolicy);

        const result = await model.generateContent(systemPrompt + "\n\nUser Request: " + prompt);
        const response = await result.response;
        const text = response.text();

        // Extract JSON from response (handle possible markdown fences)
        const cleaned = text.replace(/```json\s*/g, '').replace(/```\s*/g, '');
        const jsonMatch = cleaned.match(/\{[\s\S]*\}/);
        if (jsonMatch) {
            const parsed = JSON.parse(jsonMatch[0]);
            res.json(parsed);
        } else {
            res.json({ type: "Unknown", code: "", explanation: text });
        }
    } catch (error) {
        console.error("Generate error:", error);
        res.status(500).json({ error: error.message });
    }
});

app.get('/api/rules/opa', (req, res) => {
    const policy = readCurrentPolicy();
    if (policy) {
        res.json({ content: policy });
    } else {
        res.status(500).json({ error: "Could not read policy file" });
    }
});

app.get('/api/rules/openfga', async (req, res) => {
    res.json({ content: "// OpenFGA Model (Mock)\ntype user\ntype document\n  relations\n    define viewer: [user]\n    define editor: [user]" });
});

// Forbidden patterns that would break the policy
const FORBIDDEN_PATTERNS = [
    /^\s*package\s+/m,
    /^\s*import\s+/m,
    /^\s*default\s+(allow|authorized)\s*/m,
    /allow\s*=\s*response\s+if/,
    /input\.user\./,
    /input\.path(?!\s)/,
    /input\.method(?!\s)/,
];

app.post('/api/apply-policy', async (req, res) => {
    const { code, replaces } = req.body;
    if (!code) {
        return res.status(400).json({ success: false, error: "No policy code provided" });
    }

    // Validate: check for forbidden patterns
    for (const pattern of FORBIDDEN_PATTERNS) {
        if (pattern.test(code)) {
            return res.status(400).json({
                success: false,
                error: `Policy rejected: contains forbidden pattern "${pattern.source}". The generated rule must only add "authorized if", "is_public_path if", or helper rules.`
            });
        }
    }

    // Validate: must contain at least one meaningful rule
    const hasRule = /\b(authorized|is_public_path|is_\w+)\s+if\s*\{/.test(code);
    if (!hasRule) {
        return res.status(400).json({
            success: false,
            error: 'Policy rejected: no valid rule found. Expected "authorized if {", "is_public_path if {", or a helper rule like "is_admin if {".'
        });
    }

    try {
        let currentPolicy = readCurrentPolicy();
        if (!currentPolicy) {
            return res.status(500).json({ success: false, error: "Could not read current policy" });
        }

        const trimmedCode = code.trim();
        let updatedPolicy;

        if (replaces && replaces.trim()) {
            // REPLACE mode: find and replace the existing rule
            const replaceText = replaces.trim();
            if (!currentPolicy.includes(replaceText)) {
                // Try a more lenient match: normalize whitespace
                const normalizeWs = (s) => s.replace(/\s+/g, ' ').trim();
                const normalizedPolicy = normalizeWs(currentPolicy);
                const normalizedReplace = normalizeWs(replaceText);

                if (!normalizedPolicy.includes(normalizedReplace)) {
                    return res.status(400).json({
                        success: false,
                        error: "Could not find the rule to replace in the current policy. The 'replaces' text must match exactly."
                    });
                }

                // Find the original text using line-by-line matching
                const replaceLines = replaceText.split('\n').map(l => l.trim()).filter(l => l);
                const policyLines = currentPolicy.split('\n');
                let startIdx = -1;
                let endIdx = -1;

                for (let i = 0; i < policyLines.length; i++) {
                    if (policyLines[i].trim() === replaceLines[0]) {
                        let match = true;
                        for (let j = 0; j < replaceLines.length && (i + j) < policyLines.length; j++) {
                            if (policyLines[i + j].trim() !== replaceLines[j]) {
                                match = false;
                                break;
                            }
                        }
                        if (match) {
                            startIdx = i;
                            endIdx = i + replaceLines.length;
                            break;
                        }
                    }
                }

                if (startIdx >= 0) {
                    const before = policyLines.slice(0, startIdx).join('\n');
                    const after = policyLines.slice(endIdx).join('\n');
                    updatedPolicy = before + '\n' + trimmedCode + '\n' + after;
                } else {
                    return res.status(400).json({
                        success: false,
                        error: "Could not locate the rule to replace. Please try regenerating the rule."
                    });
                }
            } else {
                // Exact match found — direct replacement
                updatedPolicy = currentPolicy.replace(replaceText, trimmedCode);
            }
        } else {
            // APPEND mode: add to the end
            updatedPolicy = currentPolicy.trimEnd() + '\n\n' + trimmedCode + '\n';
        }

        // Write the updated policy
        fs.writeFileSync(POLICY_PATH, updatedPolicy, 'utf8');

        const mode = (replaces && replaces.trim()) ? "replaced" : "appended";
        console.log(`Policy ${mode} successfully. Pushing to OPA...`);

        // Push the updated policy to OPA via its REST API
        // (file watching doesn't work reliably in container volume mounts)
        const OPA_URL = process.env.OPA_URL || 'http://opa:8181';
        try {
            await axios.put(`${OPA_URL}/v1/policies/policy.rego`, updatedPolicy, {
                headers: { 'Content-Type': 'text/plain' }
            });
            console.log('Policy pushed to OPA successfully.');
        } catch (opaErr) {
            console.error('Failed to push policy to OPA:', opaErr.response?.data || opaErr.message);
            return res.status(500).json({
                success: false,
                error: `Policy written to disk but failed to push to OPA: ${opaErr.response?.data?.message || opaErr.message}`
            });
        }

        res.json({ success: true, message: `Policy ${mode} and pushed to OPA.` });
    } catch (err) {
        console.error("Failed to apply policy:", err);
        res.status(500).json({ success: false, error: err.message });
    }
});

app.post('/api/chat', async (req, res) => {
    const { message, context } = req.body;
    try {
        const model = genAI.getGenerativeModel({ model: "gemini-2.0-flash" });

        let systemPrompt = `You are an expert Authorization Assistant.
        You have access to the current OPA Policy and OpenFGA Model.
        Answer the user's questions about the rules, security implications, or how to modify them.

        Current OPA Policy:
        ${context.opa || "N/A"}

        Current OpenFGA Model:
        ${context.openfga || "N/A"}
        `;

        const result = await model.generateContent(systemPrompt + "\nUser: " + message);
        const response = await result.response;
        res.json({ reply: response.text() });
    } catch (error) {
        console.error(error);
        res.status(500).json({ error: error.message });
    }
});

// Push the current policy to OPA on startup (with retries for OPA readiness)
async function pushPolicyToOPA() {
    const OPA_URL = process.env.OPA_URL || 'http://opa:8181';
    const policy = readCurrentPolicy();
    if (!policy) {
        console.error('No policy file found to push to OPA');
        return;
    }

    for (let attempt = 1; attempt <= 10; attempt++) {
        try {
            await axios.put(`${OPA_URL}/v1/policies/policy.rego`, policy, {
                headers: { 'Content-Type': 'text/plain' }
            });
            console.log('Policy pushed to OPA successfully on startup.');
            return;
        } catch (err) {
            console.log(`OPA not ready (attempt ${attempt}/10): ${err.message}`);
            await new Promise(r => setTimeout(r, 3000));
        }
    }
    console.error('Failed to push policy to OPA after 10 attempts.');
}

app.listen(port, () => {
    console.log(`AI Manager listening on port ${port}`);
    pushPolicyToOPA();
});
