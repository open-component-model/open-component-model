// Custom JS for OCM website
// Necessity: Sidebar section links (<a> inside <summary>) need special click
// handling. Without this, clicking the link text toggles the <details> element
// instead of navigating to the section overview page.
//
// Uses CAPTURING phase so we intercept the click BEFORE the browser's native
// <summary> toggle fires. This prevents the <details> from flashing open/closed
// during navigation.

document.addEventListener('click', (e) => {
  const link = e.target.closest('.section-nav details > summary a.docs-link');
  if (!link) return;

  // Let the browser handle modifier-clicks (new tab, new window, etc.)
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  if (link.target && link.target !== '_self') return;
  if (link.hasAttribute('download')) return;

  const details = link.closest('details');

  // If already on this page, let the native <summary> toggle through
  // by NOT calling preventDefault — just stop the link from navigating.
  const onSamePage =
    link.getAttribute('aria-current') === 'page' ||
    new URL(link.href, location.href).pathname === location.pathname;

  if (onSamePage) {
    // Don't navigate, but allow the native <details> toggle to happen.
    // We only need to prevent the <a> default (navigation).
    // Since we're in capture phase, preventDefault here stops BOTH the
    // link navigation AND the summary toggle. So instead, we toggle manually.
    e.preventDefault();
    e.stopImmediatePropagation();
    if (details) {
      details.open = !details.open;
    }
    return;
  }

  // Navigating to a different page: prevent both toggle and link default,
  // then navigate programmatically.
  e.preventDefault();
  e.stopImmediatePropagation();
  window.location.href = link.href;
}, true); // <-- capture phase
