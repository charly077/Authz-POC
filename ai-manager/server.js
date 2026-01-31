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

// CORS for test-app at :8000
app.use((req, res, next) => {
    res.header('Access-Control-Allow-Origin', 'http://localhost:8000');
    res.header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
    res.header('Access-Control-Allow-Headers', 'Content-Type');
    if (req.method === 'OPTIONS') {
        return res.sendStatus(200);
    }
    next();
});

const genAI = new GoogleGenerativeAI(process.env.GEMINI_API_KEY || "YOUR_API_KEY");

const POLICY_PATH = '/policies/policy.rego';
const OPENFGA_URL = process.env.OPENFGA_URL || 'http://openfga:8080';
const CONFIG_FILE = '/shared/openfga-store.json';

// ──────────────────────────────────────
// OpenFGA config (read from shared volume)
// ──────────────────────────────────────
let fgaStoreId = null;
let fgaModelId = null;
let fgaReady = false;

async function loadFGAConfig() {
    for (let attempt = 1; attempt <= 30; attempt++) {
        try {
            if (fs.existsSync(CONFIG_FILE)) {
                const cfg = JSON.parse(fs.readFileSync(CONFIG_FILE, 'utf8'));
                if (cfg.storeId && cfg.modelId) {
                    fgaStoreId = cfg.storeId;
                    fgaModelId = cfg.modelId;
                    fgaReady = true;
                    console.log(`Loaded OpenFGA config: store=${fgaStoreId} model=${fgaModelId}`);
                    return;
                }
            }
        } catch (e) {
            // not ready yet
        }
        console.log(`Waiting for OpenFGA config (${attempt}/30)...`);
        await new Promise(r => setTimeout(r, 3000));
    }
    console.error('Failed to load OpenFGA config after 30 attempts.');
}

// ──────────────────────────────────────
// OPA policy management
// ──────────────────────────────────────

function readCurrentPolicy() {
    try {
        return fs.readFileSync(POLICY_PATH, 'utf8');
    } catch (err) {
        return null;
    }
}

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
- "/animals" → any authenticated user
- "/api/animals/*" → any authenticated user

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
    if (!fgaReady) {
        return res.json({ content: "// OpenFGA not initialized yet" });
    }
    try {
        const result = await axios.get(`${OPENFGA_URL}/stores/${fgaStoreId}/authorization-models`);
        const models = result.data.authorization_models || [];
        if (models.length > 0) {
            res.json({ content: JSON.stringify(models[0], null, 2) });
        } else {
            res.json({ content: "// No authorization models found" });
        }
    } catch (e) {
        res.json({ content: `// Error fetching model: ${e.message}` });
    }
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

    for (const pattern of FORBIDDEN_PATTERNS) {
        if (pattern.test(code)) {
            return res.status(400).json({
                success: false,
                error: `Policy rejected: contains forbidden pattern "${pattern.source}". The generated rule must only add "authorized if", "is_public_path if", or helper rules.`
            });
        }
    }

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
            const replaceText = replaces.trim();
            if (!currentPolicy.includes(replaceText)) {
                const normalizeWs = (s) => s.replace(/\s+/g, ' ').trim();
                const normalizedPolicy = normalizeWs(currentPolicy);
                const normalizedReplace = normalizeWs(replaceText);

                if (!normalizedPolicy.includes(normalizedReplace)) {
                    return res.status(400).json({
                        success: false,
                        error: "Could not find the rule to replace in the current policy. The 'replaces' text must match exactly."
                    });
                }

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
                updatedPolicy = currentPolicy.replace(replaceText, trimmedCode);
            }
        } else {
            updatedPolicy = currentPolicy.trimEnd() + '\n\n' + trimmedCode + '\n';
        }

        fs.writeFileSync(POLICY_PATH, updatedPolicy, 'utf8');

        const mode = (replaces && replaces.trim()) ? "replaced" : "appended";
        console.log(`Policy ${mode} successfully. Pushing to OPA...`);

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

// ──────────────────────────────────────
// Explain AuthZ endpoint (called from Animals page)
// ──────────────────────────────────────

app.post('/api/explain-authz', async (req, res) => {
    const { user, denied, deniedPath, reason, visibleAnimals, friends, myAnimalsCount, sharedAnimalsCount } = req.body;
    if (!user) {
        return res.status(400).json({ error: 'user is required' });
    }

    try {
        // Gather context
        const opaPolicy = readCurrentPolicy() || 'Policy not available';

        let fgaModel = 'Not available';
        let allTuples = [];
        let userTuples = [];

        if (fgaReady) {
            try {
                const modelRes = await axios.get(`${OPENFGA_URL}/stores/${fgaStoreId}/authorization-models/${fgaModelId}`);
                fgaModel = JSON.stringify(modelRes.data, null, 2);
            } catch (e) {
                fgaModel = `Error fetching model: ${e.message}`;
            }

            try {
                const allRes = await axios.post(`${OPENFGA_URL}/stores/${fgaStoreId}/read`, {});
                allTuples = (allRes.data.tuples || []).map(t => t.key);
            } catch (e) { /* ignore */ }

            try {
                const userRes = await axios.post(`${OPENFGA_URL}/stores/${fgaStoreId}/read`, {
                    tuple_key: { user: `user:${user}` }
                });
                userTuples = (userRes.data.tuples || []).map(t => t.key);
            } catch (e) { /* ignore */ }
        }

        let taskPrompt;
        if (denied) {
            taskPrompt = `=== SITUATION: ACCESS DENIED ===
- Username: ${user}
- Denied path: ${deniedPath}
- Denial reason: ${reason}

=== YOUR TASK ===
The user was DENIED access. Respond with EXACTLY 3 short sections using these headers. Be brief and direct — no filler text.

## Quick Explanation
One or two sentences max. Say what happened: who was denied, what path, and the one-line reason why.

## Step-by-Step: Rules That Blocked You
Walk through the relevant OPA rules for this path. Show which rule controls "${deniedPath}", what condition failed (e.g. username check), and quote the specific rule as a short code block. If OpenFGA is also relevant, mention it briefly.

## How to Fix It
One short paragraph: what specific change is needed (e.g. "add alice to the allowed users" or "add a new authorized rule"). Mention they can do this in the AuthZ Rule Builder.

IMPORTANT: Keep the ENTIRE response under 250 words. No introductions, no conclusions, no redundant text. Use bullet points and code blocks for clarity.`;
        } else {
            taskPrompt = `=== SITUATION: ACCESS GRANTED ===
- Username: ${user}
- Visible animals: ${JSON.stringify(visibleAnimals || [])}
- Friends: ${JSON.stringify(friends || [])}
- My animals count: ${myAnimalsCount || 0}
- Shared animals count: ${sharedAnimalsCount || 0}

=== YOUR TASK ===
Explain in clear, friendly language:
1. How OPA granted this user access (which policy rule matched)
2. How OpenFGA determines which animals this user can see
3. Specifically why this user sees the animals they see (trace through the tuples)
4. What the user could do to see more animals or gain edit access

Keep it concise but thorough. Use markdown with headers and bullet points.
Address the user directly ("You can see..." not "The user can see...").`;
        }

        const systemPrompt = `You are an authorization explainer for a demo application that uses a TWO-LAYER authorization system.

=== LAYER 1: OPA (Open Policy Agent) ===
OPA handles coarse-grained, path-level authorization via Envoy external authorization.
It evaluates whether a user can access an HTTP path based on JWT token claims (username, roles).

Current OPA Policy:
\`\`\`rego
${opaPolicy}
\`\`\`

=== LAYER 2: OpenFGA (Fine-Grained ReBAC) ===
OpenFGA handles relationship-based access control for the Animals feature.
It determines which animals a user can view or edit based on ownership, friendships, and relations.

Current OpenFGA Model:
${fgaModel}

All Tuples in the System:
${JSON.stringify(allTuples, null, 2)}

Tuples Involving This User (user:${user}):
${JSON.stringify(userTuples, null, 2)}

${taskPrompt}`;

        const model = genAI.getGenerativeModel({ model: "gemini-2.0-flash" });
        const result = await model.generateContent(systemPrompt);
        const response = await result.response;
        const explanation = response.text();

        res.json({ explanation });
    } catch (error) {
        console.error('Explain authz error:', error);
        res.status(500).json({ error: error.message });
    }
});

// ──────────────────────────────────────
// OpenFGA debug/management endpoints
// ──────────────────────────────────────

app.get('/api/openfga/status', (req, res) => {
    res.json({ ready: fgaReady, storeId: fgaStoreId, modelId: fgaModelId });
});

app.get('/api/openfga/tuples', async (req, res) => {
    if (!fgaReady) return res.status(503).json({ error: 'OpenFGA not ready' });
    try {
        const body = {};
        const { user, relation, object } = req.query;
        if (user || relation || object) {
            body.tuple_key = {};
            if (user) body.tuple_key.user = user;
            if (relation) body.tuple_key.relation = relation;
            if (object) body.tuple_key.object = object;
        }
        const result = await axios.post(`${OPENFGA_URL}/stores/${fgaStoreId}/read`, body);
        const tuples = (result.data.tuples || []).map(t => t.key);
        res.json({ tuples });
    } catch (e) {
        res.status(500).json({ error: e.response?.data?.message || e.message });
    }
});

app.post('/api/openfga/tuples', async (req, res) => {
    if (!fgaReady) return res.status(503).json({ error: 'OpenFGA not ready' });
    const { user, relation, object } = req.body;
    if (!user || !relation || !object) {
        return res.status(400).json({ error: 'user, relation, and object are required' });
    }
    try {
        await axios.post(`${OPENFGA_URL}/stores/${fgaStoreId}/write`, {
            writes: { tuple_keys: [{ user, relation, object }] }
        });
        res.json({ success: true });
    } catch (e) {
        res.status(500).json({ error: e.response?.data?.message || e.message });
    }
});

app.delete('/api/openfga/tuples', async (req, res) => {
    if (!fgaReady) return res.status(503).json({ error: 'OpenFGA not ready' });
    const { user, relation, object } = req.body;
    if (!user || !relation || !object) {
        return res.status(400).json({ error: 'user, relation, and object are required' });
    }
    try {
        await axios.post(`${OPENFGA_URL}/stores/${fgaStoreId}/write`, {
            deletes: { tuple_keys: [{ user, relation, object }] }
        });
        res.json({ success: true });
    } catch (e) {
        res.status(500).json({ error: e.response?.data?.message || e.message });
    }
});

app.get('/api/openfga/model', async (req, res) => {
    if (!fgaReady) return res.status(503).json({ error: 'OpenFGA not ready' });
    try {
        const result = await axios.get(`${OPENFGA_URL}/stores/${fgaStoreId}/authorization-models/${fgaModelId}`);
        res.json(result.data);
    } catch (e) {
        res.status(500).json({ error: e.response?.data?.message || e.message });
    }
});

app.get('/api/openfga/check', async (req, res) => {
    if (!fgaReady) return res.status(503).json({ error: 'OpenFGA not ready' });
    const { user, relation, object } = req.query;
    if (!user || !relation || !object) {
        return res.status(400).json({ error: 'user, relation, and object query params are required' });
    }
    try {
        const result = await axios.post(`${OPENFGA_URL}/stores/${fgaStoreId}/check`, {
            tuple_key: { user, relation, object },
            authorization_model_id: fgaModelId
        });
        res.json({ allowed: result.data.allowed === true, resolution: result.data.resolution });
    } catch (e) {
        res.status(500).json({ error: e.response?.data?.message || e.message });
    }
});

// ──────────────────────────────────────
// Visualization: OPA policy parser
// ──────────────────────────────────────

app.get('/api/visualize/opa', (req, res) => {
    const policy = readCurrentPolicy();
    if (!policy) {
        return res.status(500).json({ error: 'Could not read policy file' });
    }

    try {
        const publicPaths = [];
        const rules = [];

        const lines = policy.split('\n');
        let i = 0;

        while (i < lines.length) {
            const line = lines[i];

            // Match is_public_path blocks
            if (/^\s*is_public_path\s+if\s*\{/.test(line)) {
                const block = extractBlock(lines, i);
                const startMatch = block.body.match(/startswith\s*\(\s*http_request\.path\s*,\s*"([^"]+)"\s*\)/);
                if (startMatch) {
                    publicPaths.push(startMatch[1]);
                }
                i = block.endLine + 1;
                continue;
            }

            // Match authorized if blocks
            if (/^\s*authorized\s+if\s*\{/.test(line)) {
                const block = extractBlock(lines, i);
                const body = block.body;

                // Extract path condition
                const exactMatch = body.match(/http_request\.path\s*==\s*"([^"]+)"/);
                const prefixMatch = body.match(/startswith\s*\(\s*http_request\.path\s*,\s*"([^"]+)"\s*\)/);
                const path = exactMatch ? exactMatch[1] : (prefixMatch ? prefixMatch[1] + '*' : null);
                const matchType = exactMatch ? 'exact' : (prefixMatch ? 'prefix' : 'unknown');

                // Extract condition
                let condition = 'authenticated';
                const userMatch = body.match(/token_payload\.preferred_username\s*==\s*"([^"]+)"/);
                const roleMatch = body.match(/role\s*==\s*"([^"]+)"/);
                const helperMatch = body.match(/\b(is_\w+)\b/);

                if (userMatch) {
                    condition = `user="${userMatch[1]}"`;
                } else if (roleMatch) {
                    condition = `role="${roleMatch[1]}"`;
                } else if (helperMatch && helperMatch[1] !== 'is_public_path') {
                    condition = helperMatch[1];
                }

                if (path) {
                    // Extract comment from line above the block
                    let comment = '';
                    if (block.startLine > 0) {
                        const prevLine = lines[block.startLine - 1];
                        const commentMatch = prevLine.match(/^#\s*(.+)/);
                        if (commentMatch) comment = commentMatch[1].trim();
                    }
                    rules.push({ path, condition, type: matchType, comment });
                }

                i = block.endLine + 1;
                continue;
            }

            i++;
        }

        res.json({ publicPaths, rules });
    } catch (err) {
        console.error('OPA viz parse error:', err);
        res.status(500).json({ error: err.message });
    }
});

function extractBlock(lines, startLine) {
    let depth = 0;
    let body = '';
    let started = false;
    let endLine = startLine;

    for (let i = startLine; i < lines.length; i++) {
        const line = lines[i];
        for (const ch of line) {
            if (ch === '{') { depth++; started = true; }
            if (ch === '}') { depth--; }
        }
        body += line + '\n';
        if (started && depth === 0) {
            endLine = i;
            break;
        }
    }

    return { body, startLine, endLine };
}

// Push the current policy to OPA on startup
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
    loadFGAConfig();
});
