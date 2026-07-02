/*
 * Mermaid initialization.
 * -----------------------
 * Bundled by Hugo's esbuild wrapper (js.Build) at build time — the
 * `mermaid` bare specifier is resolved from node_modules/. No calls
 * to cdn.jsdelivr.net at runtime (GDPR).
 *
 * Preserves the original behaviour: pick the current Bootstrap theme,
 * cache the raw diagram source so we can re-render on theme toggle,
 * and re-run mermaid whenever data-bs-theme changes on <html>.
 */

import mermaid from 'mermaid';

function getMermaidTheme() {
  const htmlTheme = document.documentElement.getAttribute('data-bs-theme');
  if (htmlTheme === 'dark') return 'dark';
  if (htmlTheme === 'light') return 'default';
  // Auto mode — follow the system preference.
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default';
}

// Store original source before first render so re-renders on theme change
// can restore it (mermaid.run mutates the element content).
document.querySelectorAll('.mermaid').forEach((el) => {
  el.setAttribute('data-mermaid-source', el.textContent);
});

mermaid.initialize({
  startOnLoad: true,
  theme: getMermaidTheme(),
});

const observer = new MutationObserver((mutations) => {
  mutations.forEach((mutation) => {
    if (mutation.attributeName === 'data-bs-theme') {
      document.querySelectorAll('.mermaid').forEach((el) => {
        const source = el.getAttribute('data-mermaid-source');
        if (source) {
          el.removeAttribute('data-processed');
          el.textContent = source;
        }
      });
      mermaid.initialize({ theme: getMermaidTheme() });
      mermaid.run();
    }
  });
});
observer.observe(document.documentElement, { attributes: true });
