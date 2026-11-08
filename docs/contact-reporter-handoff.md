# Contact reporter isolation handoff

The optional reportNotomoIssue callback could throw through syscall/js, terminate WASM, and leave a submission Promise and UI pending. Source now constructs the finite exported submissionFailure from the original error before reporting. A helper recovers only js.Error at reporter.Invoke; other panic types are rethrown. Native recovery uses the same helper. Filtering, proof refresh, POST policy, and localized UI stay unchanged.

Primary owns consumer integration, Jenkins delivery, live acceptance and Notomo closure. Graphics main 0f004d55fbf7743b04503b79f3956c961761abdf already delivered the older acfbb65 artifacts via Jenkins 6 (user-provided state, not rechecked live). Motherboard staging exception remains pending. Neither consumer was edited. Original historical relay cause remains unproven; this is no closure claim.

The archive contains exactly four generated public runtime files under contact-embed/, READY-CONTACT-REPORTER.json, this handoff, build metadata and bounded verification stdout. No customer data, credentials or private evidence is included. Source has no tracked generated embed outputs, so the four binaries are supplied in this collection packet instead of introducing a second generated-output contract into the source repo.

Use Go 1.26.7 and a clean checkout at the exact source_sha/tested_sha in READY. Git revision/time and clean status are embedded into WASM; building the later evidence commit produces different bytes. Keep temporary outputs outside the checkout. The manifest source_revision is a content digest, not a Git SHA. Verify all four hashes after regeneration.

```bash
git clone https://github.com/Leopere/motherboardrepair-contact.git /tmp/contact-reporter-source
git -C /tmp/contact-reporter-source checkout --detach 28320636bbb021197649d8f7abdcbe0fa2e3a724
cd /tmp/contact-reporter-source
test -z "$(git status --porcelain)"
export GOTOOLCHAIN=go1.26.7
make verify
./scripts/sync.sh /tmp/contact-reporter-review-embed
```

Primary's exact consumer regeneration commands, after its integration approval:

```bash
# Run from the clean, pinned source checkout above. Set absolute consumer paths.
./scripts/sync.sh "$CONSUMER/docs/contact-embed"
./scripts/sync.sh "$GRAPHICS/site/contact-embed"
```

Each copies only contact-form.wasm, wasm_exec.js, loader.js and manifest.json. Motherboard references under docs/ and locales/templates/ must use loader query key 2f727ac6a132 (first 12 of loader_sha256). Graphics currently uses /contact-embed/loader.js without a query; primary owns its publication/cache contract. The loader uses full WASM/runtime digests listed in READY. Run each consumer's existing affected integrity/cache-key/build/browser checks before its normal delivery. No deployment commands were run by this source task.

Offline browser reproduction (read-only fixture checkouts at SHAs in READY):

```bash
npm install --prefix /tmp/contact-browser playwright@1.65.0
/tmp/contact-browser/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/contact-browser/node_modules \
CONTACT_CONSUMER=/path/to/staging-motherboardrepair-ca \
CONTACT_RELAY=/path/to/hearth-home-contact-relay \
CONTACT_GRAPHICS=/path/to/graphicsrepair-ca make browser-test
```

The 35 existing cases retain finite metadata, status/code/form-ID filtering and privacy checks. Twelve added cases cover real native/Graphics wrappers with throwing captureError across success, refreshed success, terminal HTTP, network and repeated proof rejection; exported invalid payload; and a directly throwing native reporter. Each checks the same WASM instance can submit successfully again, with exact proof/POST/report counts, retained failure input, successful reset and restored button. Reporter-private sentinel must never appear in rejection message/details, UI or browser errors. No report is attempted on successful or refreshed-success submissions.
