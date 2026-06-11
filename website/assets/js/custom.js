import { Tab } from 'bootstrap';

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

// Deep links into tab panes: anchors inside a non-active Bootstrap tab pane
// are display:none, so the browser cannot scroll to them natively and the
// link silently loses its target. Resolve the hash by activating every
// ancestor tab pane of the target, then scroll to it. Works stateless on a
// first visit: the URL fragment is the only input.
function revealHashTarget() {
  let id;
  try {
    id = decodeURIComponent(window.location.hash.slice(1));
  } catch {
    return;
  }
  if (!id) return;
  const target = document.getElementById(id);
  if (!target) return;

  let shown = false;
  // closest() includes the target itself, so linking to a pane id
  // (e.g. #tabs-mygroup-1) activates that tab as well.
  for (let pane = target.closest('.tab-pane'); pane; pane = pane.parentElement?.closest('.tab-pane')) {
    if (pane.classList.contains('active') || !pane.id) continue;
    const trigger = document.querySelector(`[data-bs-target="#${CSS.escape(pane.id)}"]`);
    if (trigger) {
      Tab.getOrCreateInstance(trigger).show();
      shown = true;
    }
  }
  if (shown) {
    // Wait a frame so the pane is rendered (display:block) before scrolling.
    requestAnimationFrame(() => target.scrollIntoView());
  }
}

window.addEventListener('hashchange', revealHashTarget);
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', revealHashTarget);
} else {
  revealHashTarget();
}
