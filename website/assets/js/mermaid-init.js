// Self-hosted Mermaid bundle.
//
// Replaces both @thulite/doks-core's mermaid-init.js (which imports from
// cdn.jsdelivr.net) and the inline CDN <script type="module"> that used to
// live in layouts/_default/baseof.html. Bundled through Hugo's js.Build
// pipeline so the `import` resolves against ./node_modules at build time.
//
// Listens for both the doks-core 'themeChanged' event and direct mutations
// of the data-bs-theme attribute so theme switches propagate regardless of
// which mechanism fires them.

import mermaid from 'mermaid';

const elements = Array.from(document.querySelectorAll('.mermaid'));
if (elements.length) {
  // Cache the original mermaid source so subsequent re-renders can restore it.
  elements.forEach((el) => {
    el.setAttribute('data-mermaid-source', el.textContent);
  });

  const getTheme = () => {
    const t = document.documentElement.getAttribute('data-bs-theme');
    if (t === 'dark') return 'dark';
    if (t === 'light') return 'default';
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default';
  };

  const rerender = () => {
    document.querySelectorAll('.mermaid').forEach((el) => {
      const src = el.getAttribute('data-mermaid-source');
      if (src) {
        el.removeAttribute('data-processed');
        el.textContent = src;
      }
    });
    mermaid.initialize({ theme: getTheme() });
    mermaid.run();
  };

  mermaid.initialize({ startOnLoad: true, theme: getTheme() });

  document.addEventListener('themeChanged', rerender);

  const observer = new MutationObserver((mutations) => {
    for (const m of mutations) {
      if (m.attributeName === 'data-bs-theme') {
        rerender();
        return;
      }
    }
  });
  observer.observe(document.documentElement, { attributes: true });
}
