// ──────────────────────────────────────
// Auth check — redirect to login if not authenticated
// ──────────────────────────────────────

(async function checkAuth() {
    try {
        const res = await fetch('auth/status');
        const data = await res.json();
        if (!data.authenticated) {
            window.location.href = 'auth/login';
            return;
        }
    } catch (e) {
        console.error('Auth check failed:', e);
        window.location.href = 'auth/login';
        return;
    }
})();

// Wrap fetch to handle 401s globally
const _origFetch = window.fetch;
window.fetch = async function (...args) {
    const res = await _origFetch.apply(this, args);
    if (res.status === 401) {
        window.location.href = 'auth/login';
    }
    return res;
};

const generateBtn = document.getElementById('generateBtn');
const applyBtn = document.getElementById('applyBtn');
const promptInput = document.getElementById('promptInput');
const outputCard = document.getElementById('outputCard');
const codeOutput = document.getElementById('codeOutput');
const explanation = document.getElementById('explanation');
const typeBadge = document.getElementById('typeBadge');

// Tab Switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));

        btn.classList.add('active');
        document.getElementById(btn.dataset.tab).classList.add('active');

        switch (btn.dataset.tab) {
            case 'chat':
                loadRules();
                break;
            case 'openfga':
                loadFGAStatus();
                loadTuples();
                loadModel();
                break;
            case 'visualize':
                renderAllViz();
                break;
        }
    });
});

// ──────────────────────────────────────
// OpenFGA Manager Tab
// ──────────────────────────────────────

async function loadFGAStatus() {
    try {
        const res = await fetch('api/openfga/status');
        const data = await res.json();
        const el = document.getElementById('fgaStatusText');
        if (data.ready) {
            el.innerHTML = `<span class="fga-ready">Connected</span> &mdash; Store: <code>${data.storeId}</code> &mdash; Model: <code>${data.modelId}</code>`;
        } else {
            el.innerHTML = '<span class="fga-not-ready">Not Ready</span> &mdash; Waiting for OpenFGA init...';
        }
    } catch {
        document.getElementById('fgaStatusText').textContent = 'Error connecting to server';
    }
}

async function loadTuples(filters) {
    try {
        const params = new URLSearchParams();
        if (filters?.user) params.set('user', filters.user);
        if (filters?.relation) params.set('relation', filters.relation);
        if (filters?.object) params.set('object', filters.object);
        const query = params.toString();
        const url = 'api/openfga/tuples' + (query ? '?' + query : '');

        const res = await fetch(url);
        const data = await res.json();
        const tbody = document.getElementById('tuplesBody');
        const tuples = data.tuples || [];

        if (tuples.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4" class="muted">No tuples found</td></tr>';
            return;
        }

        tbody.innerHTML = tuples.map(t =>
            `<tr>
                <td>${escapeHtml(t.user)}</td>
                <td>${escapeHtml(t.relation)}</td>
                <td>${escapeHtml(t.object)}</td>
                <td><button class="danger-btn small-btn" onclick="deleteTuple('${escapeAttr(t.user)}','${escapeAttr(t.relation)}','${escapeAttr(t.object)}')">Delete</button></td>
            </tr>`
        ).join('');
    } catch (e) {
        showToast('Error loading tuples: ' + e.message, 'error');
    }
}

async function deleteTuple(user, relation, object) {
    try {
        const res = await fetch('api/openfga/tuples', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ user, relation, object })
        });
        if (res.ok) {
            showToast('Tuple deleted');
            loadTuples();
        } else {
            const d = await res.json();
            showToast(d.error || 'Failed to delete', 'error');
        }
    } catch (e) {
        showToast('Error: ' + e.message, 'error');
    }
}

document.getElementById('addTupleBtn').addEventListener('click', async () => {
    const user = document.getElementById('addUser').value.trim();
    const relation = document.getElementById('addRelation').value.trim();
    const object = document.getElementById('addObject').value.trim();
    if (!user || !relation || !object) {
        showToast('All fields are required', 'error');
        return;
    }
    try {
        const res = await fetch('api/openfga/tuples', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ user, relation, object })
        });
        if (res.ok) {
            showToast('Tuple added!');
            document.getElementById('addUser').value = '';
            document.getElementById('addRelation').value = '';
            document.getElementById('addObject').value = '';
            loadTuples();
        } else {
            const d = await res.json();
            showToast(d.error || 'Failed to add', 'error');
        }
    } catch (e) {
        showToast('Error: ' + e.message, 'error');
    }
});

document.getElementById('filterTuplesBtn').addEventListener('click', () => {
    loadTuples({
        user: document.getElementById('filterUser').value.trim(),
        relation: document.getElementById('filterRelation').value.trim(),
        object: document.getElementById('filterObject').value.trim()
    });
});

document.getElementById('refreshTuplesBtn').addEventListener('click', () => {
    document.getElementById('filterUser').value = '';
    document.getElementById('filterRelation').value = '';
    document.getElementById('filterObject').value = '';
    loadTuples();
});

document.getElementById('checkBtn').addEventListener('click', async () => {
    const user = document.getElementById('checkUser').value.trim();
    const relation = document.getElementById('checkRelation').value.trim();
    const object = document.getElementById('checkObject').value.trim();
    if (!user || !relation || !object) {
        showToast('All fields are required', 'error');
        return;
    }
    const resultEl = document.getElementById('checkResult');
    resultEl.innerHTML = 'Checking...';
    try {
        const res = await fetch(`api/openfga/check?user=${encodeURIComponent(user)}&relation=${encodeURIComponent(relation)}&object=${encodeURIComponent(object)}`);
        const data = await res.json();
        if (data.error) {
            resultEl.innerHTML = `<span class="fga-denied">Error: ${escapeHtml(data.error)}</span>`;
        } else if (data.allowed) {
            resultEl.innerHTML = `<span class="fga-allowed">ALLOWED</span> &mdash; <code>${escapeHtml(user)}</code> has <code>${escapeHtml(relation)}</code> on <code>${escapeHtml(object)}</code>`;
        } else {
            resultEl.innerHTML = `<span class="fga-denied">DENIED</span> &mdash; <code>${escapeHtml(user)}</code> does NOT have <code>${escapeHtml(relation)}</code> on <code>${escapeHtml(object)}</code>`;
        }
    } catch (e) {
        resultEl.innerHTML = `<span class="fga-denied">Error: ${escapeHtml(e.message)}</span>`;
    }
});

async function loadModel() {
    try {
        const res = await fetch('api/openfga/model');
        const data = await res.json();
        document.getElementById('fgaModelContent').textContent = JSON.stringify(data, null, 2);
    } catch {
        document.getElementById('fgaModelContent').textContent = 'Error loading model';
    }
}

document.getElementById('refreshModelBtn').addEventListener('click', loadModel);

// ──────────────────────────────────────
// Chat Logic
// ──────────────────────────────────────
const opaContent = document.getElementById('opaContent');
const chatHistory = document.getElementById('chatHistory');
const chatInput = document.getElementById('chatInput');
const sendChatBtn = document.getElementById('sendChatBtn');

let currentContext = { opa: "", openfga: "" };
let lastGeneratedCode = "";
let lastGeneratedType = "";
let lastGeneratedReplaces = "";

async function loadRules() {
    try {
        const [opaRes, fgaRes] = await Promise.all([
            fetch('api/rules/opa'),
            fetch('api/rules/openfga')
        ]);
        const opaData = await opaRes.json();
        const fgaData = await fgaRes.json();

        currentContext.opa = opaData.content;
        currentContext.openfga = fgaData.content;

        opaContent.textContent = currentContext.opa;
    } catch (e) {
        console.error("Failed to load rules", e);
        opaContent.textContent = "Error loading rules.";
    }
}

chatInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendChatBtn.click();
    }
});

sendChatBtn.addEventListener('click', async () => {
    const message = chatInput.value.trim();
    if (!message) return;

    addMessage(message, 'user');
    chatInput.value = '';

    const loadingMsg = addMessage("Thinking...", 'ai', true);

    try {
        const res = await fetch('api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message, context: currentContext })
        });
        const data = await res.json();
        loadingMsg.remove();
        if (data.error) {
            addMessage("Error: " + data.error, 'ai');
        } else {
            addMessage(data.reply, 'ai');
        }
    } catch (e) {
        loadingMsg.remove();
        addMessage("Error communicating with AI.", 'ai');
    }
});

function addMessage(text, role, isLoading = false) {
    const div = document.createElement('div');
    div.className = `message ${role}`;
    if (isLoading) div.classList.add('loading');

    if (role === 'ai' && !isLoading) {
        div.innerHTML = renderMarkdown(text);
    } else {
        div.textContent = text;
    }

    chatHistory.appendChild(div);
    chatHistory.scrollTop = chatHistory.scrollHeight;
    return div;
}

function renderMarkdown(text) {
    let html = text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');

    html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (_, lang, code) => {
        return `<pre><code class="lang-${lang || 'text'}">${code.trim()}</code></pre>`;
    });

    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
    html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    html = html.replace(/(?<!\*)\*([^*]+)\*(?!\*)/g, '<em>$1</em>');

    html = html.replace(/^### (.+)$/gm, '<h4>$1</h4>');
    html = html.replace(/^## (.+)$/gm, '<h3>$1</h3>');
    html = html.replace(/^# (.+)$/gm, '<h2>$1</h2>');

    html = html.replace(/^(\d+)\.\s+(.+)$/gm, '<li class="ol-item" value="$1">$2</li>');
    html = html.replace(/((?:<li class="ol-item"[^>]*>.*<\/li>\n?)+)/g, '<ol>$1</ol>');

    html = html.replace(/^[\*\-]\s+(.+)$/gm, '<li>$1</li>');
    html = html.replace(/((?:<li>(?:(?!class=).)*<\/li>\n?)+)/g, '<ul>$1</ul>');

    html = html.replace(/\n{2,}/g, '</p><p>');

    html = html.replace(/(?<!<\/pre>|<\/li>|<\/ol>|<\/ul>|<\/h[234]>|<p>|<\/p>)\n(?!<pre|<ol|<ul|<li|<h[234]|<\/p>|<p>)/g, '<br>');

    if (!html.startsWith('<h') && !html.startsWith('<pre') && !html.startsWith('<ol') && !html.startsWith('<ul')) {
        html = '<p>' + html + '</p>';
    }

    html = html.replace(/<p>\s*<\/p>/g, '');

    return html;
}

// ──────────────────────────────────────
// Helpers
// ──────────────────────────────────────

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function escapeAttr(str) {
    return str.replace(/'/g, "\\'").replace(/"/g, '&quot;');
}

function showToast(message, type = 'success') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
}

// ──────────────────────────────────────
// Generate Rule
// ──────────────────────────────────────

generateBtn.addEventListener('click', async () => {
    const prompt = promptInput.value.trim();
    if (!prompt) return;

    generateBtn.textContent = "Generating...";
    generateBtn.disabled = true;

    try {
        const res = await fetch('api/generate-rule', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prompt })
        });
        const data = await res.json();

        if (data.error) {
            codeOutput.textContent = "Error: " + data.error;
            return;
        }

        outputCard.classList.remove('output-disabled');

        typeBadge.textContent = data.type || "Unknown";
        codeOutput.textContent = data.code || "No code generated.";
        explanation.textContent = data.explanation || "";

        lastGeneratedCode = data.code || "";
        lastGeneratedType = data.type || "";
        lastGeneratedReplaces = data.replaces || "";

        if (lastGeneratedReplaces) {
            applyBtn.textContent = "Apply Policy (Replace)";
        } else {
            applyBtn.textContent = "Apply Policy (Append)";
        }

    } catch (err) {
        console.error(err);
        codeOutput.textContent = "Error generating rule.";
    } finally {
        generateBtn.textContent = 'Generate Rule';
        generateBtn.disabled = false;
    }
});

// ──────────────────────────────────────
// Visualization Tab
// ──────────────────────────────────────

let vizRenderCounter = 0;

async function renderMermaidDiagram(containerId, def) {
    const container = document.getElementById(containerId);
    if (!container) return;
    container.innerHTML = '<p class="muted">Rendering...</p>';

    try {
        await waitForMermaid();
        vizRenderCounter++;
        const svgId = containerId + '-svg-' + vizRenderCounter;
        const { svg } = await window.mermaid.render(svgId, def);
        container.innerHTML = svg;
    } catch (err) {
        container.innerHTML = `<p class="muted">Diagram error: ${escapeHtml(err.message || String(err))}</p>`;
    }
}

function waitForMermaid() {
    return new Promise((resolve, reject) => {
        let attempts = 0;
        const check = () => {
            if (window.mermaid && window.mermaid.render) {
                resolve();
            } else if (attempts++ > 50) {
                reject(new Error('Mermaid failed to load'));
            } else {
                setTimeout(check, 100);
            }
        };
        check();
    });
}

function renderArchitectureDiagram() {
    const def = `flowchart LR
    Browser["Browser<br/>(User)"]:::client --> Envoy["Envoy<br/>Proxy"]:::proxy
    Envoy -->|"OAuth2 filter<br/>redirect if no token"| KC["Keycloak<br/>(IdP)"]:::kc
    KC -->|"JWT token<br/>via /callback"| Envoy
    Envoy -->|"ext_authz<br/>gRPC"| OPA["OPA<br/>(Policy Engine)"]:::opa
    OPA -->|"allow / deny"| Envoy
    Envoy -->|"if allowed<br/>(forwards JWT)"| App["Node.js<br/>App"]:::app
    App -->|"ReBAC checks"| FGA["OpenFGA<br/>(Fine-Grained)"]:::fga

    classDef client fill:#3b82f6,stroke:#1d4ed8,color:#fff
    classDef proxy fill:#8b5cf6,stroke:#6d28d9,color:#fff
    classDef opa fill:#22d3ee,stroke:#0891b2,color:#0f172a
    classDef app fill:#10b981,stroke:#059669,color:#fff
    classDef fga fill:#f59e0b,stroke:#d97706,color:#0f172a
    classDef kc fill:#ef4444,stroke:#dc2626,color:#fff`;

    renderMermaidDiagram('archContainer', def);
}

async function renderOpaViz() {
    const container = document.getElementById('opaVizContainer');
    container.innerHTML = '<p class="muted">Loading OPA policy...</p>';

    try {
        const res = await fetch('api/visualize/opa');
        const data = await res.json();
        if (data.error) {
            container.innerHTML = `<p class="muted">Error: ${escapeHtml(data.error)}</p>`;
            return;
        }

        const { publicPaths, rules } = data;

        let def = 'flowchart TD\n';
        def += '    REQ["Incoming<br/>Request"]:::req --> PUB{"Public path?"}:::decision\n';

        // Public paths
        if (publicPaths.length > 0) {
            const pubLabel = publicPaths.join(', ');
            def += `    PUB -->|"Yes: ${pubLabel}"| ALLOW_PUB["Allow<br/>(no auth)"]:::allow\n`;
        }
        def += '    PUB -->|"No"| TOKEN{"Valid JWT<br/>token?"}:::decision\n';
        def += '    TOKEN -->|"No"| DENY_TOKEN["Deny<br/>(401)"]:::deny\n';
        def += '    TOKEN -->|"Yes"| RULES{"Path rules"}:::decision\n';

        // Per-path rules
        rules.forEach((rule, idx) => {
            const nodeId = `R${idx}`;
            const condLabel = rule.condition === 'authenticated' ? 'any user' : rule.condition;
            const pathLabel = rule.path.replace(/"/g, '');
            def += `    RULES --> ${nodeId}["${pathLabel}<br/>${condLabel}"]:::rule\n`;
            def += `    ${nodeId} --> ALLOW["Allow"]:::allow\n`;
        });

        def += '    RULES -->|"no match"| DENY["Deny<br/>(403)"]:::deny\n';

        def += `
    classDef req fill:#3b82f6,stroke:#1d4ed8,color:#fff
    classDef decision fill:#8b5cf6,stroke:#6d28d9,color:#fff
    classDef allow fill:#10b981,stroke:#059669,color:#fff
    classDef deny fill:#ef4444,stroke:#dc2626,color:#fff
    classDef rule fill:#1e293b,stroke:#475569,color:#e2e8f0`;

        await renderMermaidDiagram('opaVizContainer', def);
    } catch (err) {
        container.innerHTML = `<p class="muted">Error: ${escapeHtml(err.message)}</p>`;
    }
}

async function renderFgaViz() {
    const container = document.getElementById('fgaVizContainer');
    container.innerHTML = '<p class="muted">Loading OpenFGA data...</p>';

    try {
        const [modelRes, tuplesRes] = await Promise.all([
            fetch('api/openfga/model'),
            fetch('api/openfga/tuples')
        ]);
        const modelData = await modelRes.json();
        const tuplesData = await tuplesRes.json();

        if (modelData.error) {
            container.innerHTML = `<p class="muted">Error: ${escapeHtml(modelData.error)}</p>`;
            return;
        }

        const model = modelData.authorization_model || modelData;
        const typeDefs = model.type_definitions || [];
        const tuples = tuplesData.tuples || [];

        let def = `flowchart LR\n`;

        // Build type definition subgraphs
        typeDefs.forEach((td) => {
            const typeName = td.type;
            const relations = td.metadata?.relations || {};
            const relNames = Object.keys(relations);

            if (relNames.length === 0) {
                def += `    ${typeName}["${typeName}"]:::typebox\n`;
            } else {
                def += `    subgraph ${typeName}["${typeName}"]\n`;
                relNames.forEach((rel) => {
                    const nodeId = `${typeName}_${rel}`;
                    def += `        ${nodeId}["${rel}"]:::rel\n`;
                });
                def += `    end\n`;
            }
        });

        // Build tuple edges — use a single node per entity regardless of user/object role
        const nodeIds = new Map();
        function ensureNode(fullId) {
            const sanitized = `n_${sanitizeId(fullId)}`;
            if (!nodeIds.has(fullId)) {
                nodeIds.set(fullId, sanitized);
                const parts = fullId.split(':');
                const type = parts[0] || 'unknown';
                const id = parts[1] || fullId;
                def += `    ${sanitized}["${id}<br/>(${type})"]:::entity\n`;
            }
            return nodeIds.get(fullId);
        }

        tuples.forEach((t) => {
            const userNode = ensureNode(t.user);
            const objNode = ensureNode(t.object);
            def += `    ${userNode} -->|"${t.relation}"| ${objNode}\n`;
        });

        if (tuples.length === 0 && typeDefs.length === 0) {
            container.innerHTML = '<p class="muted">No OpenFGA data available</p>';
            return;
        }

        def += `
    classDef typebox fill:#1e293b,stroke:#8b5cf6,color:#c4b5fd
    classDef rel fill:#1e293b,stroke:#475569,color:#94a3b8
    classDef entity fill:#1e293b,stroke:#3b82f6,color:#93c5fd`;

        await renderMermaidDiagram('fgaVizContainer', def);
    } catch (err) {
        container.innerHTML = `<p class="muted">Error: ${escapeHtml(err.message)}</p>`;
    }
}

function sanitizeId(str) {
    return str.replace(/[^a-zA-Z0-9]/g, '_');
}

function renderAllViz() {
    renderArchitectureDiagram();
    renderOpaViz();
    renderFgaViz();
}

// Visualization refresh buttons
document.getElementById('refreshArchBtn')?.addEventListener('click', renderArchitectureDiagram);
document.getElementById('refreshOpaVizBtn')?.addEventListener('click', renderOpaViz);
document.getElementById('refreshFgaVizBtn')?.addEventListener('click', renderFgaViz);

// ──────────────────────────────────────
// Generate / Apply Rule
// ──────────────────────────────────────

applyBtn.addEventListener('click', async () => {
    if (!lastGeneratedCode) {
        showToast("No policy to apply.", "error");
        return;
    }

    if (lastGeneratedType !== "OPA") {
        showToast("Only OPA policies can be applied directly.", "error");
        return;
    }

    applyBtn.textContent = "Applying...";
    applyBtn.disabled = true;

    try {
        const res = await fetch('api/apply-policy', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code: lastGeneratedCode, replaces: lastGeneratedReplaces })
        });
        const data = await res.json();

        if (data.success) {
            showToast("Policy applied successfully!");
        } else {
            showToast("Failed to apply: " + (data.error || "Unknown error"), "error");
        }
    } catch (err) {
        console.error(err);
        showToast("Error applying policy.", "error");
    } finally {
        applyBtn.textContent = "Apply Policy";
        applyBtn.disabled = false;
    }
});
