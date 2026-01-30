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

        if (btn.dataset.tab === 'chat') {
            loadRules();
        }
    });
});

// Chat Logic
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
            fetch('/api/rules/opa'),
            fetch('/api/rules/openfga')
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

// Enter key support for chat
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
        const res = await fetch('/api/chat', {
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

    // User messages stay plain text, AI messages get markdown rendering
    if (role === 'ai' && !isLoading) {
        div.innerHTML = renderMarkdown(text);
    } else {
        div.textContent = text;
    }

    chatHistory.appendChild(div);
    chatHistory.scrollTop = chatHistory.scrollHeight;
    return div;
}

// Lightweight markdown renderer for chat messages
function renderMarkdown(text) {
    // Escape HTML first to prevent XSS
    let html = text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');

    // Code blocks (``` ... ```)
    html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (_, lang, code) => {
        return `<pre><code class="lang-${lang || 'text'}">${code.trim()}</code></pre>`;
    });

    // Inline code (`...`)
    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');

    // Bold (**...**)
    html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');

    // Italic (*...*)
    html = html.replace(/(?<!\*)\*([^*]+)\*(?!\*)/g, '<em>$1</em>');

    // Headers (###, ##, #)
    html = html.replace(/^### (.+)$/gm, '<h4>$1</h4>');
    html = html.replace(/^## (.+)$/gm, '<h3>$1</h3>');
    html = html.replace(/^# (.+)$/gm, '<h2>$1</h2>');

    // Numbered lists (1. item)
    html = html.replace(/^(\d+)\.\s+(.+)$/gm, '<li class="ol-item" value="$1">$2</li>');
    html = html.replace(/((?:<li class="ol-item"[^>]*>.*<\/li>\n?)+)/g, '<ol>$1</ol>');

    // Unordered lists (* item or - item)
    html = html.replace(/^[\*\-]\s+(.+)$/gm, '<li>$1</li>');
    html = html.replace(/((?:<li>(?:(?!class=).)*<\/li>\n?)+)/g, '<ul>$1</ul>');

    // Paragraphs: convert double newlines to paragraph breaks
    html = html.replace(/\n{2,}/g, '</p><p>');

    // Single newlines to <br> (but not inside pre/code blocks)
    html = html.replace(/(?<!<\/pre>|<\/li>|<\/ol>|<\/ul>|<\/h[234]>|<p>|<\/p>)\n(?!<pre|<ol|<ul|<li|<h[234]|<\/p>|<p>)/g, '<br>');

    // Wrap in paragraph if not already wrapped in block elements
    if (!html.startsWith('<h') && !html.startsWith('<pre') && !html.startsWith('<ol') && !html.startsWith('<ul')) {
        html = '<p>' + html + '</p>';
    }

    // Clean up empty paragraphs
    html = html.replace(/<p>\s*<\/p>/g, '');

    return html;
}

function showToast(message, type = 'success') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
}

// Generate Rule
generateBtn.addEventListener('click', async () => {
    const prompt = promptInput.value.trim();
    if (!prompt) return;

    generateBtn.textContent = "Generating...";
    generateBtn.disabled = true;

    try {
        const res = await fetch('/api/generate-rule', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prompt })
        });
        const data = await res.json();

        if (data.error) {
            codeOutput.textContent = "Error: " + data.error;
            return;
        }

        outputCard.style.opacity = '1';
        outputCard.style.pointerEvents = 'all';

        typeBadge.textContent = data.type || "Unknown";
        codeOutput.textContent = data.code || "No code generated.";
        explanation.textContent = data.explanation || "";

        lastGeneratedCode = data.code || "";
        lastGeneratedType = data.type || "";
        lastGeneratedReplaces = data.replaces || "";

        // Show mode indicator
        if (lastGeneratedReplaces) {
            applyBtn.textContent = "Apply Policy (Replace)";
        } else {
            applyBtn.textContent = "Apply Policy (Append)";
        }

    } catch (err) {
        console.error(err);
        codeOutput.textContent = "Error generating rule.";
    } finally {
        generateBtn.innerHTML = 'Generate Rule <span class="icon">&#10024;</span>';
        generateBtn.disabled = false;
    }
});

// Apply Policy - writes generated OPA policy to the server
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
        const res = await fetch('/api/apply-policy', {
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
