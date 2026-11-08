# Contact submission failure investigation

The reported site-2 `/contact.html` event is real. Its underlying cause cannot be determined from the old event: `handleSubmit` dropped the error and reported only `component`, `form_id`, and `stage`. This change repairs that diagnostic loss; it does not establish that the customer's submission problem is fixed. Primary owns review, Jenkins delivery, live acceptance, and Notomo closure.

## Inspected sources and offline evidence

- Runtime baseline: `Leopere/motherboardrepair-contact` main `d1e230f3171890096634b808f9408efd28b3b7fb`.
- Read-only consumer: `Leopere/staging-motherboardrepair-ca` staging `7d5de862274ce72e3bf237aabea2a56902933342`; `docs/contact-embed/manifest.json` declares this runtime repository as owner, content revision `sha256:30b079e3c09babf64aa281eb6e0409622b494c90bb8814cb01246a4e4f29190b`. Recomputing the existing sync algorithm over the canonical baseline exactly matches this content digest. It is not a Git SHA or proof of the currently deployed runtime revision.
- Read-only relay: `Leopere/hearth-home-contact-relay` main `2ff5a1ad076787d287835512d40655b22b95dded`, `worker/public-intake.js`, `worker/abuse-protection.js`, `worker/lead-relay.js`, `worker/public-validation.js`, and `wrangler.leads.toml`. Checked-out source/config is not evidence of the currently deployed version.
- No AGENTS.md was present in the runtime, consumer, relay, or workspace ancestor directories. Runtime README/Makefile/build/sync/verification workflow and consumer DEVELOPMENT.md/TESTING.md standards were read.

The client gets `/api/form-proof` with GET, caches until one minute before expiry, waits until `ready_at` (milliseconds), and hashes the JSON string array `["mrc-form-proof-v1", form_id/source, name/full_name, email, phone/phone_number, company, message/notes, slack_profile]`. It uses SHA-256, base64url without padding for the binding digest, and leading-zero bits of SHA-256(`token.digest.counter`), with counter at most 10,000,000. The relay signs the challenge using its own HMAC secret, origin, client binding, nonce, and timestamps; minimum age is 2.5 seconds, lifetime one hour, configured difficulty 13. The browser never owns that secret or nonce issuance. No proof-security change is proposed.

POST `/api/submit` sends JSON with the token/counter. Each request has an existing 20-second AbortController deadline, covering headers and body reading. Only the first `403` with exact `FORM_PROOF_INVALID` gets one fresh challenge and one retry. Transport failures, timeout, HTTP errors, malformed JSON, and other rejections do not retry. Success requires HTTP 2xx AND JSON `ok: true` AND `success: true`. The inspected public relay wraps successful durable Workflow queueing as HTTP 202 with both flags true. Rejections include 400 validation, 403 origin/proof, 429 rate limit, and 503 missing proof secret/token/Workflow or queue failure. Only the proof rejections currently have structured codes; arbitrary error messages cannot safely identify other causes.

Offline browser tests load the actual consumer contact page, its source assets and Notomo wrapper, and newly built runtime from disk. Every browser request is fulfilled from disk/in-memory handlers or aborted; service workers are blocked, remote DNS is disabled, and Node fetch and Cloudflare socket use throw. The actual public relay handler and HMAC verifier run with synthetic secrets, fake form values, and an in-memory queue stub. No production forms, customer data, credential APIs, email, bridges, Jenkins, or infrastructure mutations are used. A queued success and invalid-proof refresh succeed under that contract; failures are deliberately injected. This does not reproduce the original event's cause or establish live delivery.

## Safe diagnostic contract

Event name remains `contact_form_submission_failed`; component remains `contact_form` and stage remains `submit`. Only source-owned form IDs are reported; others become `unknown`.

`status` is a number from 100 through 599, or 0 when no HTTP status is available. The existing consumer wrapper stringifies it for Notomo. `code` is a finite failure class, optionally suffixed with `:FORM_PROOF_INVALID` or `:FORM_PROOF_UNAVAILABLE`. No arbitrary relay code or error text is included. This encoding uses the consumer's existing five-field allowlist; additional field names would be silently dropped.

Allowed classes:

- `proof_network`, `proof_timeout`, `proof_response_read`, `proof_response_json`, `proof_http`, `proof_challenge`, `proof_compute`
- `submit_network`, `submit_timeout`, `submit_response_read`, `submit_response_json`, `submit_http`, `submit_rejected`
- `unknown`

Network failure means browser fetch rejection, which may include CORS, DNS, TLS, or connectivity; it does not prove any one of them. Timeout is identified by this request's aborted signal. JSON parsing failures retain the HTTP status but no response text. A successful-HTTP response without both success flags is `submit_rejected`. Existing localized UI messages, input retention on failure, success reset, button recovery, and proof retry rules are preserved. An ambiguous failed acknowledgement must not cause an automatic resend.

## Reproduce offline

Use Go 1.26.7, GNU Make, Node 26, and Playwright 1.65.0/Chromium. Install test dependencies outside both source checkouts if desired:

```bash
npm install --prefix /tmp/contact-browser playwright@1.65.0
/tmp/contact-browser/node_modules/.bin/playwright install chromium
make verify
NODE_PATH=/tmp/contact-browser/node_modules \
CONTACT_CONSUMER=/path/to/read-only-staging-checkout \
CONTACT_RELAY=/path/to/read-only-relay-checkout \
make browser-test
```

The browser matrix covers real queued 202, invalid-proof refresh, proof HTTP/JSON/schema/network failures, repeated proof rejection, proof unavailable, rate limiting, unknown-code filtering, submit non-JSON, missing success flag, network rejection, real 20-second timeout, and response read rejection. It checks exact metadata before and after the consumer's Notomo allowlist, no private fixture text, no extra POST retry, input retention, and button restoration.

For baseline comparison, build the baseline in a separate extracted source tree (run its build script from that tree), then run the same test with `CONTACT_DIST=/path/to/baseline/dist CONTACT_BASELINE=1`. This asserts diagnostic loss in the baseline, rather than expecting the original unknown failure to recur. Native Go tests cover metadata filtering, arbitrary error wrappers/IDs/codes/status, proof binding/coercion and solving. The read-only relay's `node --test test/abuse-protection.test.js test/lead-relay.test.js` covers signing/client/timing rejection and durable queue contracts with fake dependencies.

## Primary read-only log investigation

For the original event's UTC time window, inspect the public host `forms.motherboardrepair.ca`, Cloudflare Worker service **website-lead-intake**, route `/api/form-proof` or `/api/submit`, configured by `wrangler.leads.toml` in the relay source above. This is the first receiving service, not an assumed DigitalOcean form daemon. Record the active Cloudflare version ID (the service exposes it on read-only `GET /version`) and map that deployed version to its relay Git source revision; preserve the source/consumer runtime content revision separately. No health request was needed or made during this investigation.

Request only a redacted projection of existing logs: UTC timestamp, service/host, route, method, numeric HTTP status, request duration bucket, active Worker version/source revision, finite operation outcome (`origin_denied`, `rate_limited`, `parse_rejected`, `proof_unavailable`, `proof_invalid`, `validation_rejected`, `lead_token_unconfigured`, `workflow_unconfigured`, `workflow_queue_failed`, `workflow_queued`, `exception`, `unknown`) where already available, and allowlisted proof code. Missing fields must be marked unavailable, not inferred from customer content. For an OPTIONS/CORS failure, request the public origin allowlist/config and presence/absence of CORS headers, not visitor identifiers. For proof failures, request only pass/fail categories for signature, lifetime/minimum age, origin, client binding, difficulty/counter and payload binding if existing structured logs provide them; never the token, signature, IP, binding, nonce, or payload. Current source does not log all such categories, so causal separation may remain impossible.

For queue failures, project only whether a configured `LEAD_DELIVERY` binding and queue operation succeeded and whether a queue exception occurred. Do not retrieve Workflow payloads, lead/customer records, delivery IDs, raw exception text, headers, query strings, IPs, session/visitor identifiers, secrets, or response bodies. Downstream `website-lead-intake-delivery`/`LeadDeliveryWorkflow` starts after successful queue acknowledgement and does not establish why this submit-stage event occurred. A timeout or lost body may follow successful queueing; primary must account for that ambiguity before any user-directed retry. The new diagnostic contract is for future occurrences and cannot recover the old discarded status.

## Consumer delivery responsibility

Only runtime source main is changed/pushed by this task. No consumer/staging/production/relay files are edited or pushed. After primary reviews the concrete source patch and authorizes a consumer embed update, primary runs from the approved runtime source SHA:

```bash
make verify
./scripts/sync.sh "$CONSUMER/docs/contact-embed"
```

This updates exactly the runtime-owned `contact-form.wasm`, matching `wasm_exec.js`, digest-substituted `loader.js`, and content-digest `manifest.json`. Primary must also update every `/contact-embed/loader.js?v=...` reference in consumer `docs/` and `locales/templates/` HTML/JS to the first 12 hex characters of the new manifest's `loader_sha256`. The sync script does not update those references. Run the consumer's existing embed integrity/cache-key tests, source/localization/build checks, and intercepted browser form checks, and review generated artifacts/references before its ordinary delivery flow. Do not hand-edit generated WASM or migrate CI.

The tracked consumer `.deploy-it.json` delegates publication to `./scripts/deploy-jenkins.sh`, which invokes `/opt/homebrew/bin/python3 /Users/aedev/.local/share/jenkins-local/jenkins-delivery.py deploy-motherboardrepair-ca`. Primary owns that existing Jenkins handoff after the approved consumer patch and normal delivery workflow; this runner must not invoke it. Source main push alone cannot update the website's committed embed. A production submission, acceptance check, or Notomo closure requires primary's own authorization and review.

## Recorded validation on this runner

Go 1.26.7 `make verify` passed (native tests, js/wasm vet, tracked build and WASM magic check). All 15 patched browser scenarios passed in Chromium with Playwright 1.65.0, including metadata after the unmodified consumer Notomo wrapper. The inspected relay's 31 proof/queue regression tests passed using fake dependencies. Baseline comparison and final source commit are recorded in `READY-CONTACT.json`; the underlying event cause remains unknown. Local verification dependencies were extracted into temporary runner directories without sudo or infrastructure mutation. Review embed artifacts were generated under `/tmp/contact-review-embed`, never into the consumer checkout.
