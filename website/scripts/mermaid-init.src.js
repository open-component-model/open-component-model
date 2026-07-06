// Source for assets/js/mermaid-init.js.
//
// Why this lives in scripts/ rather than assets/js/:
//   The file imports mermaid as a bare specifier (`import mermaid from
//   'mermaid'`), which only a bundler can resolve. doks-core's stock
//   footer/script-footer.html serves js/mermaid-init.js via
//   `<script type="module" src=...>` - the browser then tries to fetch a
//   URL literally called "mermaid" and fails. So we bundle this source with
//   esbuild during `npm install` (see scripts/bundle-mermaid.js) into a
//   self-contained ESM module at assets/js/mermaid-init.js, which the stock
//   doks-core partial can then serve verbatim (no project-level override of
//   script-footer.html required, no drift risk on doks-core bumps).
//
// Why we replace mermaid at all:
//   doks-core's own mermaid-init.js imports mermaid from cdn.jsdelivr.net,
//   which is a third-party call at page-load time. GDPR compliance for the
//   OCM site requires zero outbound calls to third-party hosts.
//
// Why the theme handling here has both an event listener and a mutation
// observer: doks-core historically fired a 'themeChanged' event, but the
// theme is stored on <html data-bs-theme>. Listening for both means the
// re-render fires regardless of which mechanism a future doks-core bump
// keeps.

import mermaid from 'mermaid';

// Why we don't rely on mermaid's own `startOnLoad: true`:
//   `startOnLoad` registers a single 'load' / 'DOMContentLoaded' listener at
//   the moment mermaid.initialize() is called. If this script has been
//   deferred (type="module" scripts always are) or delayed by the browser
//   for any reason, that event has already fired by the time we register -
//   and the listener never runs, leaving .mermaid blocks as raw text. Drive
//   run() ourselves and gate on document.readyState instead.
const start = () => {
  const elements = Array.from(document.querySelectorAll('.mermaid'));
  if (!elements.length) return;

  // Cache the mermaid source on each element BEFORE the first render, so a
  // subsequent theme-change re-render can restore the pre-SVG text. Once
  // mermaid processes an element, its textContent becomes the rendered SVG
  // and the original diagram source is lost.
  elements.forEach((el) => {
    el.setAttribute('data-mermaid-source', el.textContent);
  });

  const getTheme = () => {
    const t = document.documentElement.getAttribute('data-bs-theme');
    if (t === 'dark') return 'dark';
    if (t === 'light') return 'default';
    // "auto" - fall back to the OS-level preference so the initial render
    // matches what doks-core's theme toggle would resolve to.
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default';
  };

  const rerender = () => {
    document.querySelectorAll('.mermaid').forEach((el) => {
      const src = el.getAttribute('data-mermaid-source');
      if (src) {
        // Undo mermaid's "already rendered" guard and restore the source so
        // mermaid.run() picks the element up again with the new theme.
        el.removeAttribute('data-processed');
        el.textContent = src;
      }
    });
    mermaid.initialize({ theme: getTheme() });
    mermaid.run();
  };

  mermaid.initialize({ startOnLoad: false, theme: getTheme() });
  mermaid.run();

  document.addEventListener('themeChanged', rerender);

  // Belt-and-suspenders: catch theme changes made by writing directly to the
  // <html data-bs-theme> attribute (some third-party theme toggles bypass
  // the doks-core 'themeChanged' event).
  const observer = new MutationObserver((mutations) => {
    for (const m of mutations) {
      if (m.attributeName === 'data-bs-theme') {
        rerender();
        return;
      }
    }
  });
  observer.observe(document.documentElement, { attributes: true });
};

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', start, { once: true });
} else {
  start();
}
