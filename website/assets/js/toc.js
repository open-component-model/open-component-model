// Local override of @thulite/doks-core assets/js/toc.js.
//
// Hugo builds .TableOfContents from the page's own markdown AST. Tab panes are
// rendered outside that AST -- layouts/_shortcodes/tabs.html calls RenderString
// once per tab -- so every heading inside a tab, and inside the steps nested in
// them, is missing from the server-rendered TOC (see ocm-project#1152).
//
// Every pane is present in the DOM (inactive ones are only hidden by CSS), so
// the complete outline can be rebuilt from the rendered page and swapped into
// the TOC. Pages without tabs keep the server-rendered TOC and the theme's
// scroll spy untouched.

import { ScrollSpy } from 'bootstrap';

// Mirrors markup.tableOfContents in config/_default/markup.yaml. Hugo does not
// expose that config to templates, so the bounds are repeated here.
const START_LEVEL = 2;
const END_LEVEL = 3;

// Subtrees that are page chrome rather than page content.
const SKIP = '.toc-mobile, .nav-tabs, .page-nav, .page-footer-meta';

// A heading counts as reached once it is this far into the viewport. Roughly
// the height of the sticky site header.
const ACTIVATION_OFFSET = 96;

function headingText(heading) {
  const clone = heading.cloneNode(true);
  for (const anchor of clone.querySelectorAll('.anchor')) {
    anchor.remove();
  }
  return clone.textContent.trim();
}

// The pane label lives on the nav button; the hidden .tab-pane-label paragraph
// (rendered for the no-JS fallback) is the backstop for theme-rendered tabs.
function paneLabel(pane) {
  const trigger = document.querySelector(`[data-bs-target="#${CSS.escape(pane.id)}"]`);
  const label = trigger ?? pane.querySelector(':scope > .tab-pane-label');
  return label ? label.textContent.trim() : pane.id;
}

// Outline of one scope: the page content, or a single tab pane. Headings nest
// by their level. A tab group nests its panes one level below the heading it
// follows, and every pane is a scope of its own, so an <h2> inside a tab stays
// under its tab instead of escaping to the page level.
function buildOutline(root) {
  const items = [];
  const stack = [{ level: START_LEVEL - 1, children: items }];

  function add(item, level) {
    while (stack.length > 1 && stack[stack.length - 1].level >= level) {
      stack.pop();
    }
    stack[stack.length - 1].children.push(item);
    stack.push({ level, children: item.children });
  }

  function walk(node) {
    for (const child of node.children) {
      if (child.matches(SKIP)) continue;

      if (child.classList.contains('tab-content')) {
        // Fixed for the whole group so that the panes end up as siblings.
        const level = stack[stack.length - 1].level + 1;
        for (const pane of child.children) {
          if (!pane.classList.contains('tab-pane') || !pane.id) continue;
          add({ id: pane.id, text: paneLabel(pane), children: buildOutline(pane) }, level);
        }
        continue;
      }

      const heading = /^H([1-6])$/.exec(child.tagName);
      if (heading) {
        const level = Number(heading[1]);
        if (child.id && level >= START_LEVEL && level <= END_LEVEL) {
          add({ id: child.id, text: headingText(child), children: [] }, level);
        }
        continue;
      }

      if (child.firstElementChild) walk(child);
    }
  }

  walk(root);
  return items;
}

// Mirrors the markup of Hugo's .TableOfContents (markup.tableOfContents.ordered
// is false) so that the existing TOC styles keep working.
function renderOutline(items) {
  const list = document.createElement('ul');
  for (const item of items) {
    const entry = document.createElement('li');
    const link = document.createElement('a');
    link.href = `#${item.id}`;
    link.textContent = item.text;
    entry.append(link);
    if (item.children.length) {
      entry.append(renderOutline(item.children));
    }
    list.append(entry);
  }
  return list;
}

function flattenIds(items, ids = []) {
  for (const item of items) {
    ids.push(item.id);
    flattenIds(item.children, ids);
  }
  return ids;
}

// Bootstrap's scroll spy orders the observed elements by offsetTop, which is
// measured against the nearest positioned ancestor: inside a step every
// heading reports 0 (the <li> is positioned for its number badge), so tab
// content never activates. Spy on viewport positions instead. Headings in an
// inactive pane have no box and are skipped until their tab is shown.
function spyOnOutline(ids, links) {
  const headings = ids.map((id) => document.getElementById(id)).filter(Boolean);
  let active = null;
  let queued = false;

  function update() {
    queued = false;

    let current = null;
    for (const heading of headings) {
      if (!heading.getClientRects().length) continue;
      if (heading.getBoundingClientRect().top > ACTIVATION_OFFSET) {
        // Nothing reached yet: keep the first entry highlighted.
        current ??= heading;
        break;
      }
      current = heading;
    }

    if (current === active) return;
    for (const link of links.get(active?.id) ?? []) {
      link.classList.remove('active');
    }
    for (const link of links.get(current?.id) ?? []) {
      link.classList.add('active');
    }
    active = current;
  }

  function schedule() {
    if (queued) return;
    queued = true;
    requestAnimationFrame(update);
  }

  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', schedule, { passive: true });
  // Showing a tab moves everything below it, and reveals its own headings.
  document.addEventListener('shown.bs.tab', schedule);
  // Mermaid diagrams and web fonts settle after DOMContentLoaded.
  window.addEventListener('load', schedule);
  update();
}

function renderTabAwareToc() {
  const content = document.querySelector('main.docs-content');
  if (!content || !content.querySelector('.tab-content')) return;

  const outline = buildOutline(content);
  if (!outline.length) return;

  // Desktop (#toc) and mobile (#TableOfContents) both render into .page-links.
  const navs = document.querySelectorAll('.page-links > nav');
  if (!navs.length) return;

  const links = new Map();
  for (const nav of navs) {
    nav.replaceChildren(renderOutline(outline));
    for (const link of nav.querySelectorAll('a')) {
      const id = decodeURIComponent(link.hash.slice(1));
      links.set(id, [...(links.get(id) ?? []), link]);
    }
  }

  // Keep the theme's scroll spy from claiming this TOC. The data API picks the
  // attribute up on window load, after this runs; dispose covers the case
  // where the bundle only finishes loading after that.
  document.body.removeAttribute('data-bs-spy');
  window.addEventListener('load', () => ScrollSpy.getInstance(document.body)?.dispose());

  spyOnOutline(flattenIds(outline), links);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', renderTabAwareToc);
} else {
  renderTabAwareToc();
}

// Close mobile TOC details when clicking on a link
document.addEventListener('click', function (e) {
  // Check if the clicked element is a link within the mobile TOC
  const tocMobile = e.target.closest('.toc-mobile');
  if (tocMobile && e.target.tagName === 'A') {
    // Find the details element within the mobile TOC
    const details = tocMobile.querySelector('details');
    if (details && details.open) {
      // Close the details element
      details.open = false;
    }
  }
});
