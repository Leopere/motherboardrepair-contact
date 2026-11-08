# Motherboard Repair Contact

Go/WebAssembly contact-form runtime for Motherboard Repair Canada. It owns the protected form transport, validation, proof-of-work challenge, bounded request timeouts, and success semantics. The website keeps its localized HTML form and automatically falls back to the previous JavaScript runtime if WebAssembly cannot start.

## Commands

```bash
make verify
make sync
```

`make sync` builds with the installed Go toolchain and copies the versioned runtime artifacts into the sibling `motherboardrepair-ca/docs/contact-embed` directory. The website commits those artifacts so its production build does not depend on a cross-repository checkout.

## License

Copyright © 2026 ColinKnapp.com. All rights reserved.

No license is granted except as required by applicable law or the terms of the hosting platform. Contact ColinKnapp.com for written permission before you use, copy, modify, or distribute this work.

Offline contact-failure diagnosis, bounded Notomo metadata, browser regression setup, and the primary-owned consumer/Jenkins handoff are documented in [the contact failure runbook](docs/contact-failure-runbook.md).
