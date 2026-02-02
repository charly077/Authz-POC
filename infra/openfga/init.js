const http = require('http');
const fs = require('fs');
const path = require('path');

const OPENFGA_URL = process.env.OPENFGA_URL || 'http://openfga:8080';
const SHARED_DIR = '/shared';
const CONFIG_FILE = path.join(SHARED_DIR, 'openfga-store.json');
const MAX_RETRIES = 20;
const RETRY_DELAY = 3000;

function request(method, urlPath, body) {
    return new Promise((resolve, reject) => {
        const url = new URL(urlPath, OPENFGA_URL);
        const options = {
            hostname: url.hostname,
            port: url.port,
            path: url.pathname,
            method,
            headers: { 'Content-Type': 'application/json' }
        };
        const req = http.request(options, (res) => {
            let data = '';
            res.on('data', chunk => data += chunk);
            res.on('end', () => {
                try {
                    resolve({ status: res.statusCode, data: JSON.parse(data) });
                } catch {
                    resolve({ status: res.statusCode, data });
                }
            });
        });
        req.on('error', reject);
        if (body) req.write(JSON.stringify(body));
        req.end();
    });
}

async function sleep(ms) {
    return new Promise(r => setTimeout(r, ms));
}

async function waitForOpenFGA() {
    for (let i = 1; i <= MAX_RETRIES; i++) {
        try {
            const res = await request('GET', '/healthz');
            if (res.status === 200) {
                console.log('OpenFGA is ready.');
                return;
            }
        } catch (e) {
            // not ready yet
        }
        console.log(`Waiting for OpenFGA... (${i}/${MAX_RETRIES})`);
        await sleep(RETRY_DELAY);
    }
    throw new Error('OpenFGA did not become ready');
}

async function findOrCreateStore(name) {
    // List existing stores
    const listRes = await request('GET', '/stores');
    if (listRes.status === 200 && listRes.data.stores) {
        const existing = listRes.data.stores.find(s => s.name === name);
        if (existing) {
            console.log(`Found existing store: ${existing.id}`);
            return existing.id;
        }
    }
    // Create new store
    const createRes = await request('POST', '/stores', { name });
    if (createRes.status !== 201 && createRes.status !== 200) {
        throw new Error(`Failed to create store: ${JSON.stringify(createRes.data)}`);
    }
    console.log(`Created store: ${createRes.data.id}`);
    return createRes.data.id;
}

async function writeAuthModel(storeId) {
    const model = {
        schema_version: '1.1',
        type_definitions: [
            {
                type: 'user',
                relations: {
                    guardian: { this: {} }
                },
                metadata: {
                    relations: {
                        guardian: { directly_related_user_types: [{ type: 'user' }] }
                    }
                }
            },
            {
                type: 'organization',
                relations: {
                    member: { this: {} },
                    admin: { this: {} },
                    can_manage: {
                        computedUserset: { relation: 'admin' }
                    }
                },
                metadata: {
                    relations: {
                        member: { directly_related_user_types: [{ type: 'user' }] },
                        admin: { directly_related_user_types: [{ type: 'user' }] }
                    }
                }
            },
            {
                type: 'dossier',
                relations: {
                    owner: { this: {} },
                    mandate_holder: { this: {} },
                    org_parent: { this: {} },
                    blocked: { this: {} },
                    public: { this: {} },
                    can_view: {
                        union: {
                            child: [
                                { this: {} },
                                { computedUserset: { relation: 'owner' } },
                                { computedUserset: { relation: 'mandate_holder' } },
                                { tupleToUserset: { tupleset: { relation: 'owner' }, computedUserset: { relation: 'guardian' } } },
                                { tupleToUserset: { tupleset: { relation: 'org_parent' }, computedUserset: { relation: 'member' } } },
                                { computedUserset: { relation: 'public' } }
                            ]
                        }
                    },
                    viewer: {
                        difference: {
                            base: { computedUserset: { relation: 'can_view' } },
                            subtract: { computedUserset: { relation: 'blocked' } }
                        }
                    },
                    editor: {
                        union: {
                            child: [
                                { this: {} },
                                { computedUserset: { relation: 'owner' } },
                                { computedUserset: { relation: 'mandate_holder' } }
                            ]
                        }
                    }
                },
                metadata: {
                    relations: {
                        owner: { directly_related_user_types: [{ type: 'user' }] },
                        mandate_holder: { directly_related_user_types: [{ type: 'user' }] },
                        org_parent: { directly_related_user_types: [{ type: 'organization' }] },
                        blocked: { directly_related_user_types: [{ type: 'user' }] },
                        public: { directly_related_user_types: [{ type: 'user', wildcard: {} }] },
                        can_view: { directly_related_user_types: [{ type: 'user' }] },
                        editor: { directly_related_user_types: [{ type: 'user' }] }
                    }
                }
            }
        ]
    };

    const res = await request('POST', `/stores/${storeId}/authorization-models`, model);
    if (res.status !== 201 && res.status !== 200) {
        throw new Error(`Failed to write auth model: ${JSON.stringify(res.data)}`);
    }
    console.log(`Authorization model created: ${res.data.authorization_model_id}`);
    return res.data.authorization_model_id;
}

async function main() {
    console.log('OpenFGA Init: starting...');

    await waitForOpenFGA();

    const storeId = await findOrCreateStore('citizen-mandate');
    const modelId = await writeAuthModel(storeId);

    // Write config to shared volume
    if (!fs.existsSync(SHARED_DIR)) {
        fs.mkdirSync(SHARED_DIR, { recursive: true });
    }
    const config = { storeId, modelId, createdAt: new Date().toISOString() };
    fs.writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2));
    console.log(`Config written to ${CONFIG_FILE}`);
    console.log('OpenFGA Init: done.');
}

main().catch(err => {
    console.error('OpenFGA Init FAILED:', err.message);
    process.exit(1);
});
