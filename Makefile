.PHONY: build test check verify sync clean browser-test

build:
	./scripts/build.sh

test:
	go test ./...

check:
	GOOS=js GOARCH=wasm go vet ./...

verify: test check build
	@test "$$(head -c 4 dist/contact-form.wasm | od -An -tx1 | tr -d ' \n')" = "0061736d"

sync: verify
	./scripts/sync.sh

clean:
	rm -rf dist

# Optional offline integration; see docs/contact-failure-runbook.md for checkouts/dependencies.
browser-test: build
	node tests/contact-browser.cjs
