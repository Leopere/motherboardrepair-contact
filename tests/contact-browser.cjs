// Offline integration against read-only website and relay source checkouts.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { registerHooks } = require('node:module');
const { chromium } = require('playwright');
const root = path.resolve(__dirname, '..');
const consumer = process.env.CONTACT_CONSUMER;
const relay = process.env.CONTACT_RELAY;
const dist = path.resolve(process.env.CONTACT_DIST || path.join(root, 'dist'));
assert(consumer && relay, 'Set CONTACT_CONSUMER and CONTACT_RELAY to read-only source checkouts');
// The public handler re-exports a Workflow class. No socket or Workflow may run.
registerHooks({ resolve(specifier, context, next) {
    if (specifier === 'cloudflare:sockets') return { url: 'data:text/javascript,export function connect(){throw Error("offline sockets forbidden")}', shortCircuit: true };
    if (specifier === 'cloudflare:workers') return { url: 'data:text/javascript,export class WorkflowEntrypoint {}', shortCircuit: true };
    return next(specifier, context);
}});
const origin = 'https://motherboardrepair.ca';
const forms = 'https://forms.motherboardrepair.ca';
const privateSentinel = 'DO_NOT_REPORT_form_token_email_body';
const mime = { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.wasm': 'application/wasm', '.json': 'application/json', '.svg': 'image/svg+xml' };

(async () => {
    const { default: handler } = await import(pathToFileURL(path.join(relay, 'worker/public-intake.js')));
    globalThis.fetch = () => { throw Error('offline Node fetch forbidden'); };
    const browser = await chromium.launch({ args: ['--host-resolver-rules=MAP * ~NOTFOUND'] });
    try {
        const cases = [
            ['queued', null], ['refresh', null],
            ['proof_http', ['proof_http', 503, '']],
            ['proof_invalid_json', ['proof_response_json', 200, '']],
            ['proof_schema', ['proof_challenge', 200, '']],
            ['proof_network', ['proof_network', 0, '']],
            ['proof_rejected', ['submit_http', 403, 'FORM_PROOF_INVALID']],
            ['unavailable', ['submit_http', 503, 'FORM_PROOF_UNAVAILABLE']],
            ['rate_limit', ['submit_http', 429, '']],
            ['unknown_code', ['submit_http', 400, '']],
            ['non_json', ['submit_response_json', 502, '']],
            ['bad_success', ['submit_rejected', 200, '']],
            ['network', ['submit_network', 0, '']],
            ['timeout', ['submit_timeout', 0, '']],
            ['body_read', ['submit_response_read', 202, '']],
        ];
        for (const [scenario, expected] of cases) {
            const context = await browser.newContext({ serviceWorkers: 'block' });
            const page = await context.newPage();
            let posts = 0, proofs = 0, queued = 0;
            const routeErrors = [];
            const env = {
                MRC_ALLOWED_ORIGINS: origin, MRC_FORM_PROOF_ENABLED: 'true',
                MRC_FORM_PROOF_ORIGINS: origin, MRC_FORM_PROOF_DIFFICULTY: '13',
                MRC_FORM_PROOF_SECRET: 'offline-test-secret-only', MRC_LEAD_TOKEN: 'offline-test-token-only',
                PUBLIC_FORM_RATE_LIMITER: { limit: async () => ({ success: scenario !== 'rate_limit' }) },
                LEAD_DELIVERY: { createBatch: async () => { queued++; return [{}]; } },
            };
            if (scenario === 'proof_http') delete env.MRC_FORM_PROOF_SECRET;
            await context.route('**/*', async route => {
                try {
                    const req = route.request();
                    const url = new URL(req.url());
                    if (url.origin === forms) {
                        const proof = url.pathname === '/api/form-proof';
                        assert(proof || url.pathname === '/api/submit');
                        assert.equal(req.method(), proof ? 'GET' : 'POST');
                        proof ? proofs++ : posts++;
                        const json = (status, body) => route.fulfill({ status, contentType: 'application/json', headers: { 'Access-Control-Allow-Origin': origin }, body: JSON.stringify(body) });
                        if ((proof && scenario === 'proof_network') || (!proof && scenario === 'network')) return route.abort('failed');
                        if (!proof && scenario === 'timeout') return; // AbortController must bound the stalled request.
                        if ((proof && scenario === 'proof_invalid_json') || (!proof && scenario === 'non_json')) return route.fulfill({ status: proof ? 200 : 502, contentType: 'text/html', body: privateSentinel, headers: { 'Access-Control-Allow-Origin': origin } });
                        if (proof && scenario === 'proof_schema') return json(200, { ok: true, challenge: '', difficulty: 13 });
                        if (!proof && (scenario === 'proof_rejected' || (scenario === 'refresh' && posts === 1))) return json(403, { ok: false, success: false, code: 'FORM_PROOF_INVALID', error: privateSentinel });
                        if (!proof && scenario === 'unknown_code') return json(400, { ok: false, success: false, code: privateSentinel, error: privateSentinel });
                        if (!proof && scenario === 'bad_success') return json(200, { ok: true, success: false, message: privateSentinel });
                        // Use the actual server handler, including HMAC, origin, age and payload proof verification.
                        const result = await handler.fetch(new Request(req.url(), {
                            method: req.method(), headers: { Origin: origin, 'Content-Type': 'application/json' },
                            ...(proof ? {} : { body: req.postData() }),
                        }), !proof && scenario === 'unavailable' ? { ...env, MRC_FORM_PROOF_SECRET: '' } : env);
                        return route.fulfill({ status: result.status, headers: Object.fromEntries(result.headers), body: await result.text() });
                    }
                    if (url.origin !== origin) return route.abort();
                    const relative = decodeURIComponent(url.pathname).replace(/^\/+/, '');
                    const base = relative.startsWith('contact-embed/') ? dist : path.join(consumer, 'docs');
                    const filename = path.resolve(base, relative.startsWith('contact-embed/') ? relative.slice('contact-embed/'.length) : relative);
                    assert(filename.startsWith(base + path.sep));
                    if (!fs.existsSync(filename) || !fs.statSync(filename).isFile()) return route.fulfill({ status: 404, body: '' });
                    return route.fulfill({ status: 200, contentType: mime[path.extname(filename)] || 'application/octet-stream', body: fs.readFileSync(filename) });
                } catch (error) { routeErrors.push(error); await route.abort(); }
            });
            if (scenario === 'body_read') await page.addInitScript(({ forms, privateSentinel }) => {
                const original = window.fetch;
                window.fetch = async (url, options) => {
                    const response = await original(url, options);
                    if (String(url) === forms + '/api/submit') return { status: response.status, text: () => Promise.reject(new Error(privateSentinel)) };
                    return response;
                };
            }, { forms, privateSentinel });
            await page.goto(origin + '/contact.html');
            await page.waitForFunction(() => window.MRCContactWASM && document.querySelector('#quoteForm').dataset.mrcWasmInitialized === 'true');
            await page.evaluate(() => {
                window.offlineIssues = [];
                window.offlineCaptured = [];
                window.notomo = { captureError: (error, metadata) => window.offlineCaptured.push([error.message, metadata]) };
                const reporter = window.reportNotomoIssue;
                if (typeof reporter !== 'function') throw Error('consumer Notomo wrapper missing');
                window.reportNotomoIssue = (...args) => { window.offlineIssues.push(args); reporter(...args); };
            });
            const form = page.locator('#quoteForm');
            // Select the visible service choice before filling newly revealed fields.
            const service = form.locator('#service_type');
            if (await service.isVisible()) await service.selectOption('In-Person');
            for (const field of await form.locator('[required]').all()) {
                if (!await field.isVisible() || !await field.isEnabled()) continue;
                const name = await field.getAttribute('name');
                const type = await field.getAttribute('type');
                const tag = await field.evaluate(el => el.tagName);
                if (type === 'checkbox' || type === 'radio') await field.check();
                else if (tag === 'SELECT') { if (name !== 'service_type') await field.selectOption({ index: 1 }); }
                else await field.fill(({ name: 'Offline Fixture', email: 'offline@example.invalid', phone: '+1 202 555 0100', device_name: 'Offline board', message: privateSentinel })[name] || 'Offline fixture');
            }
            const button = form.locator('#submit-btn');
            const label = await button.textContent();
            await button.click();
            await form.locator('#form-message.' + (expected ? 'error' : 'success')).waitFor({ state: 'visible', timeout: 35000 });
            assert.equal(await button.isEnabled(), true);
            assert.equal(await button.textContent(), label);
            const issues = await page.evaluate(() => window.offlineIssues);
            assert.deepEqual(routeErrors, []);
            if (!expected) {
                assert.equal(issues.length, 0); assert.equal(queued, 1);
                assert.equal(posts, scenario === 'refresh' ? 2 : 1);
                assert.equal(proofs, scenario === 'refresh' ? 2 : 1);
            } else {
                assert.equal(issues.length, 1);
                assert.equal(issues[0][0], 'contact_form_submission_failed');
                const metadata = issues[0][1];
                const legacy = { component: 'contact_form', form_id: 'motherboard_repair_quote', stage: 'submit' };
                if (process.env.CONTACT_BASELINE === '1') assert.deepEqual(metadata, legacy);
                else {
                    const code = expected[0] + (expected[2] ? ':' + expected[2] : '');
                    assert.deepEqual(metadata, { ...legacy, status: expected[1], code });
                    const captured = await page.evaluate(() => window.offlineCaptured);
                    assert.deepEqual(captured, [[issues[0][0], { ...legacy, status: String(expected[1]), code }]]);
                }
                assert(!JSON.stringify(issues).includes(privateSentinel));
                assert.equal(posts, scenario.startsWith('proof_') && scenario !== 'proof_rejected' ? 0 : scenario === 'proof_rejected' ? 2 : 1);
                if (scenario === 'proof_rejected') assert.equal(proofs, 2);
                // A rejection keeps the user's input for their own retry.
                assert.equal(await form.locator('#message').inputValue(), privateSentinel);
            }
            console.log('PASS ' + scenario);
            await context.close();
        }
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
