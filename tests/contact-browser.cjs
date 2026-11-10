// Offline integration against read-only website and relay source checkouts.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { execFileSync } = require('node:child_process');
const { registerHooks } = require('node:module');
const { chromium } = require('playwright');
const root = path.resolve(__dirname, '..');
const consumer = process.env.CONTACT_CONSUMER;
const relay = process.env.CONTACT_RELAY;
const graphics = process.env.CONTACT_GRAPHICS;
const dist = path.resolve(process.env.CONTACT_DIST || path.join(root, 'dist'));
assert(consumer && relay && graphics, 'Set CONTACT_CONSUMER, CONTACT_RELAY and CONTACT_GRAPHICS to read-only source checkouts');
// Render the real Graphics page in memory; -B prevents writes to its checkout.
const graphicsHTML = execFileSync('python3', ['-B', '-c',
    "import runpy,sys; m=runpy.run_path(sys.argv[1]); print(m['render_index']('en'))",
    path.join(graphics, 'build_graphics_site.py')], { encoding: 'utf8' });
const graphicsFrenchHTML = execFileSync('python3', ['-B', '-c',
    "import runpy,sys; m=runpy.run_path(sys.argv[1]); print(m['render_index']('fr'))",
    path.join(graphics, 'build_graphics_site.py')], { encoding: 'utf8' });
// The public handler re-exports a Workflow class. No socket or Workflow may run.
registerHooks({ resolve(specifier, context, next) {
    if (specifier === 'cloudflare:sockets') return { url: 'data:text/javascript,export function connect(){throw Error("offline sockets forbidden")}', shortCircuit: true };
    if (specifier === 'cloudflare:workers') return { url: 'data:text/javascript,export class WorkflowEntrypoint {}', shortCircuit: true };
    return next(specifier, context);
}});
const motherboardOrigin = 'https://motherboardrepair.ca';
const forms = 'https://forms.motherboardrepair.ca';
const privateSentinel = 'DO_NOT_REPORT_form_token_email_body';
const reporterSentinel = 'DO_NOT_REPORT_private_reporter_exception';
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
        const runs = [
            ...cases.map(([scenario, expected]) => ({ scenario, expected, graphicsPath: false })),
            ...cases.map(([scenario, expected]) => ({ scenario, expected, graphicsPath: true })),
            ...['queued', 'unavailable', 'network'].map(scenario => ({
                scenario, expected: cases.find(row => row[0] === scenario)[1], graphicsPath: true, noReporter: true
            })),
            { scenario: 'invalid_payload', expected: ['unknown', 0, ''], graphicsPath: true },
            { scenario: 'invalid_payload', expected: ['unknown', 0, ''], graphicsPath: true, noReporter: true },
        ];
        // Exercise actual consumer wrappers when captureError throws, plus a
        // directly throwing optional reporter on the native path.
        for (const graphicsPath of [false, true]) {
            for (const scenario of ['queued', 'refresh', 'unavailable', 'network', 'proof_rejected']) {
                runs.push({ scenario, expected: cases.find(row => row[0] === scenario)[1], graphicsPath, throwingReporter: true });
            }
        }
        runs.push({ scenario: 'invalid_payload', expected: ['unknown', 0, ''], graphicsPath: true, throwingReporter: true });
        runs.push({ scenario: 'unavailable', expected: ['submit_http', 503, 'FORM_PROOF_UNAVAILABLE'], graphicsPath: false, throwingReporter: true, directReporter: true });
        runs.push({ scenario: 'queued', expected: null, graphicsPath: false, clockSkew: 600 });
        for (const scenario of ['queued', 'refresh', 'proof_rejected']) {
            runs.push({ scenario, expected: cases.find(row => row[0] === scenario)[1], graphicsPath: true, french: true, clockSkew: 600 });
        }
        for (const { scenario: initialScenario, expected, graphicsPath, noReporter, throwingReporter, directReporter, french, clockSkew } of runs) {
            if (process.env.CONTACT_CLOCK_ONLY === '1' && !clockSkew) continue;
            let scenario = initialScenario;
            if (process.env.CONTACT_BASELINE === '1' && graphicsPath) continue;
            const origin = graphicsPath ? 'https://graphicsrepair.ca' : motherboardOrigin;
            const context = await browser.newContext({ serviceWorkers: 'block' });
            const page = await context.newPage();
            let posts = 0, proofs = 0, queued = 0;
            const browserErrors = [];
            page.on('pageerror', error => browserErrors.push(error.message));
            page.on('console', message => { if (message.type() === 'error') browserErrors.push(message.text()); });
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
                    if (graphicsPath && url.pathname === (french ? '/fr/' : '/')) return route.fulfill({ status: 200, contentType: 'text/html', body: french ? graphicsFrenchHTML : graphicsHTML });
                    const relative = decodeURIComponent(url.pathname).replace(/^\/+/, '');
                    const base = relative.startsWith('contact-embed/') ? dist : graphicsPath ? path.join(graphics, 'site') : path.join(consumer, 'docs');
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
            if (clockSkew) await page.addInitScript(seconds => {
                const RealDate = Date;
                window.Date = class extends RealDate {
                    constructor(...args) { super(...(args.length ? args : [RealDate.now() + seconds * 1000])); }
                    static now() { return RealDate.now() + seconds * 1000; }
                };
            }, clockSkew);
            await page.goto(origin + (graphicsPath ? (french ? '/fr/' : '/') : '/contact.html'));
            await page.waitForFunction(graphicsPath
                ? () => window.MRCContactWASM && window.ContactForm === window.MRCContactWASM
                : () => window.MRCContactWASM && document.querySelector('#quoteForm').dataset.mrcWasmInitialized === 'true');
            await page.evaluate(({ graphicsPath, noReporter, throwingReporter, directReporter, reporterSentinel }) => {
                window.offlineIssues = [];
                window.offlineCaptured = [];
                window.offlineRejections = [];
                window.notomo = { captureError: (error, metadata) => {
                    window.offlineCaptured.push([error.message, metadata]);
                    if (throwingReporter) throw new Error(reporterSentinel);
                } };
                const reporter = window.reportNotomoIssue;
                if (typeof reporter !== 'function') throw Error('consumer Notomo wrapper missing');
                if (noReporter) delete window.reportNotomoIssue;
                else window.reportNotomoIssue = (...args) => {
                    window.offlineIssues.push(args);
                    if (directReporter) throw new Error(reporterSentinel);
                    if (reporter) reporter(...args);
                };
                if (graphicsPath) {
                    const submit = window.ContactForm.submitProtectedPayload;
                    // Observe the actual Graphics caller's Promise without replacing its behavior.
                    window.ContactForm.submitProtectedPayload = function (...args) {
                        return submit(...args).catch(error => {
                            window.offlineRejections.push({ isError: error instanceof Error, message: error.message, details: error.details });
                            throw error;
                        });
                    };
                }
            }, { graphicsPath, noReporter, throwingReporter, directReporter, reporterSentinel });
            const form = page.locator(graphicsPath ? '#repair-form' : '#quoteForm');
            const service = form.locator('[name="service_type"]');
            if (await service.isVisible()) await service.selectOption('In-Person');
            for (const field of await form.locator('[required]').all()) {
                if (!await field.isVisible() || !await field.isEnabled()) continue;
                const name = await field.getAttribute('name');
                const type = await field.getAttribute('type');
                const tag = await field.evaluate(el => el.tagName);
                if (type === 'checkbox' || type === 'radio') await field.check();
                else if (tag === 'SELECT') { if (name !== 'service_type') await field.selectOption({ index: 1 }); }
                else await field.fill(({ name: 'Offline Fixture', email: 'offline@example.invalid', phone: '+1 202 555 0100', device_name: 'Offline board', model: 'Offline GPU', message: privateSentinel })[name] || 'Offline fixture');
            }
            const button = form.locator(graphicsPath ? 'button[type="submit"]' : '#submit-btn');
            const label = await button.textContent();
            if (scenario === 'invalid_payload') {
                // An arbitrary decode error must become finite unknown diagnostics.
                await page.evaluate(async privateSentinel => {
                    await window.ContactForm.submitProtectedPayload(privateSentinel).catch(() => {});
                }, privateSentinel);
            } else await button.click();
            if (scenario !== 'invalid_payload') {
                const status = form.locator((graphicsPath ? '#form-status.' : '#form-message.') + (expected ? 'error' : 'success'));
                await status.waitFor({ state: 'visible', timeout: 35000 });
                if (graphicsPath) assert.equal(await status.textContent(), await form.getAttribute(expected ? 'data-error' : 'data-success'));
            }
            assert.equal(await button.isEnabled(), true);
            assert.equal(await button.textContent(), label);
            const issues = await page.evaluate(() => window.offlineIssues);
            assert.deepEqual(routeErrors, []);
            if (!expected) {
                assert.equal(issues.length, 0); assert.equal(queued, 1);
                assert.deepEqual(await page.evaluate(() => window.offlineRejections), []);
                assert.equal(await form.locator('[name="message"]').inputValue(), '');
                assert.equal(posts, scenario === 'refresh' ? 2 : 1);
                assert.equal(proofs, scenario === 'refresh' ? 2 : 1);
            } else {
                const code = expected[0] + (expected[2] ? ':' + expected[2] : '');
                const safe = { component: 'contact_form', form_id: scenario === 'invalid_payload' ? 'unknown' : graphicsPath ? 'graphics_card_repair_quote' : 'motherboard_repair_quote', stage: 'submit', status: expected[1], code };
                if (graphicsPath) {
                    const rejections = await page.evaluate(() => window.offlineRejections);
                    assert.deepEqual(rejections, [{ isError: true, message: 'contact form submission failed: ' + code + ' (status ' + expected[1] + ')', details: safe }]);
                    assert(!JSON.stringify(rejections).includes(privateSentinel));
                }
                assert.equal(issues.length, noReporter ? 0 : 1);
                if (!noReporter) {
                    assert.equal(issues[0][0], 'contact_form_submission_failed');
                    const metadata = issues[0][1];
                    const legacy = { component: 'contact_form', form_id: 'motherboard_repair_quote', stage: 'submit' };
                    if (process.env.CONTACT_BASELINE === '1') assert.deepEqual(metadata, legacy);
                    else {
                        assert.deepEqual(metadata, safe);
                        if (!graphicsPath && !directReporter) {
                            const captured = await page.evaluate(() => window.offlineCaptured);
                            assert.deepEqual(captured, [[issues[0][0], { ...legacy, status: String(expected[1]), code }]]);
                        }
                    }
                }
                assert(!JSON.stringify(issues).includes(privateSentinel));
                assert(!JSON.stringify(issues).includes(reporterSentinel));
                assert.equal(posts, scenario === 'invalid_payload' ? 0 : scenario.startsWith('proof_') && scenario !== 'proof_rejected' ? 0 : scenario === 'proof_rejected' ? 2 : 1);
                assert.equal(proofs, scenario === 'invalid_payload' ? 0 : scenario === 'proof_rejected' || (!graphicsPath && scenario.startsWith('proof_')) ? 2 : 1);
                // A rejection keeps the user's input for their own retry.
                assert.equal(await form.locator('[name="message"]').inputValue(), privateSentinel);
            }
            if (throwingReporter) {
                const observations = await page.evaluate(() => ({
                    issues: window.offlineIssues, captured: window.offlineCaptured,
                    rejections: window.offlineRejections, text: document.body.textContent
                }));
                assert(!JSON.stringify(observations).includes(reporterSentinel));
                assert(!JSON.stringify(browserErrors).includes(reporterSentinel));
                assert.equal(observations.issues.length, expected ? 1 : 0);
                assert.equal(observations.captured.length, expected && !directReporter ? 1 : 0);
                if (!graphicsPath && expected) assert.equal(await form.locator('#form-message').textContent(),
                    'Sorry, there was an issue submitting your request. Please try again or leave a voicemail at 226-702-0555.');
                // A user-initiated second submission proves the same WASM instance
                // remains usable. The first attempt must never resend on its own.
                const before = { posts, proofs, queued };
                const instance = await page.evaluateHandle(() => window.MRCContactWASM);
                scenario = 'queued';
                await form.locator('[name="message"]').fill(privateSentinel);
                // Success reset clears required values; restore them for the next click.
                if (!expected) {
                    for (const field of await form.locator('[required]').all()) {
                        if (!await field.isVisible() || !await field.isEnabled()) continue;
                        const type = await field.getAttribute('type');
                        const name = await field.getAttribute('name');
                        const tag = await field.evaluate(el => el.tagName);
                        if (type === 'checkbox' || type === 'radio') await field.check();
                        else if (tag === 'SELECT') await field.selectOption({ index: 1 });
                        else await field.fill(({ name: 'Offline Fixture', email: 'offline@example.invalid', phone: '+1 202 555 0100', device_name: 'Offline board', model: 'Offline GPU', message: privateSentinel })[name] || 'Offline fixture');
                    }
                }
                await button.click();
                await form.locator(graphicsPath ? '#form-status.success' : '#form-message.success').waitFor({ state: 'visible', timeout: 35000 });
                assert.equal(await page.evaluate(previous => previous === window.MRCContactWASM, instance), true);
                assert.equal(posts, before.posts + 1);
                assert.equal(proofs, before.proofs + (initialScenario === 'invalid_payload' ? 1 : 0));
                assert.equal(queued, before.queued + 1);
                assert.equal(await button.isEnabled(), true);
                assert.equal(await button.textContent(), label);
                assert.equal(await form.locator('[name="message"]').inputValue(), '');
                assert.deepEqual(await page.evaluate(() => window.offlineIssues), observations.issues);
                assert.deepEqual(await page.evaluate(() => window.offlineCaptured), observations.captured);
                assert.deepEqual(await page.evaluate(() => window.offlineRejections), observations.rejections);
                assert.deepEqual(routeErrors, []);
                assert(!JSON.stringify(browserErrors).includes(reporterSentinel));
            }
            console.log('PASS ' + (graphicsPath ? 'graphics' : 'native') + (noReporter ? '/no-reporter' : '') + (throwingReporter ? '/throwing-' + (directReporter ? 'reporter' : 'capture') : '') + '/' + initialScenario);
            await context.close();
        }
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
