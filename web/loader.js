// Copyright © 2026 ColinKnapp.com. All rights reserved.

(function () {
    'use strict';

    const ASSET_ROOT = '/contact-embed/';
    const queuedForms = [];
    let implementation = null;
    let resolveReady;
    let rejectReady;
    const ready = new Promise((resolve, reject) => {
        resolveReady = resolve;
        rejectReady = reject;
    });

    function pendingSubmit(form) {
        if (form.__mrcPendingSubmit) return;
        const handler = event => {
            event.preventDefault();
            const message = form.querySelector('#form-message');
            const button = form.querySelector('#submit-btn');
            if (message) {
                message.textContent = 'Preparing the secure contact form...';
                message.className = 'form-message info';
                message.style.display = 'block';
            }
            if (button) button.disabled = true;
            ready.then(api => {
                form.removeEventListener('submit', handler, true);
                delete form.__mrcPendingSubmit;
                api.init(form);
                if (button) button.disabled = false;
                form.requestSubmit();
            }).catch(() => {
                if (button) button.disabled = false;
                if (message) {
                    message.textContent = 'The form could not start. Please refresh or leave a voicemail at 226-702-0555.';
                    message.className = 'form-message error';
                }
            });
        };
        form.__mrcPendingSubmit = handler;
        form.addEventListener('submit', handler, true);
    }

    const bridge = {
        init(form) {
            if (!form) return;
            if (implementation) {
                implementation.init(form);
                return;
            }
            if (!queuedForms.includes(form)) queuedForms.push(form);
            pendingSubmit(form);
        },
        prepareProtectedSubmission() {
            return ready.then(api => api.prepareProtectedSubmission());
        },
        submitProtectedPayload(payload) {
            return ready.then(api => api.submitProtectedPayload(payload));
        }
    };
    window.ContactForm = bridge;
    window.MRCContactReady = ready;

    function activate(api) {
        implementation = api;
        window.ContactForm = api;
        resolveReady(api);
        queuedForms.splice(0).forEach(form => {
            if (form.__mrcPendingSubmit) {
                form.removeEventListener('submit', form.__mrcPendingSubmit, true);
                delete form.__mrcPendingSubmit;
            }
            api.init(form);
        });
        const inline = document.getElementById('quoteForm');
        if (inline) api.init(inline);
    }

    function loadScript(src) {
        return new Promise((resolve, reject) => {
            const script = document.createElement('script');
            script.src = src;
            script.onload = resolve;
            script.onerror = reject;
            document.head.appendChild(script);
        });
    }

    async function instantiate(go) {
        const response = await fetch(ASSET_ROOT + 'contact-form.wasm?v=__WASM_SHA256__', { cache: 'force-cache' });
        if (!response.ok) throw new Error(`WASM HTTP ${response.status}`);
        if (WebAssembly.instantiateStreaming) {
            try {
                return await WebAssembly.instantiateStreaming(response.clone(), go.importObject);
            } catch (error) {
                // GitHub Pages normally serves application/wasm; the buffer path also
                // works if an intermediary changes the MIME type.
            }
        }
        return WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
    }

    async function startWASM() {
        await loadScript(ASSET_ROOT + 'wasm_exec.js?v=__RUNTIME_SHA256__');
        const go = new Go();
        const result = await instantiate(go);
        go.run(result.instance).catch(error => console.error('Contact WASM stopped:', error));
        for (let attempt = 0; attempt < 100 && !window.MRCContactWASM; attempt += 1) {
            await new Promise(resolve => setTimeout(resolve, 10));
        }
        if (!window.MRCContactWASM) throw new Error('Contact WASM did not initialize');
        activate(window.MRCContactWASM);
    }

    function fallbackPath() {
        const language = (document.documentElement.lang || 'en').split('-')[0].toLowerCase();
        return language && language !== 'en'
            ? `/${language}/js/contact-form.min.js`
            : '/js/contact-form.min.js';
    }

    startWASM().catch(async error => {
        console.warn('Contact WASM unavailable; using JavaScript fallback:', error);
        try {
            await loadScript(fallbackPath() + '?v=elapsed-proof-v1');
            const fallback = window.ContactForm;
            if (!fallback || fallback === bridge || typeof fallback.init !== 'function') {
                throw new Error('JavaScript fallback did not initialize');
            }
            activate(fallback);
        } catch (fallbackError) {
            rejectReady(fallbackError);
            console.error('Contact form unavailable:', fallbackError);
        }
    });
})();
