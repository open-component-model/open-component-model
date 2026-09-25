// Local override of @thulite/doks-core assets/js/toc.js.
//
// Hugo builds .TableOfContents from the page's own markdown AST. Tab panes are
// rendered outside that AST -- layouts/_shortcodes/tabs.html calls RenderString
// once per tab -- so every heading inside a tab, and inside the steps nested in
// them, is missing from the server-rendered TOC (see ocm-project#1152).
//
// Every pane is present in the DOM (inactive ones are only hidden by CSS), so
// the complete outline can be rebuilt from the rendered page and swapped into
// the TOC. Listing every tab would make the TOC longer than the page it
// describes, so each tab's outline is collapsible and follows its tab: only the
// shown pane is expanded. Pages without tabs keep the server-rendered TOC and
// the theme's scroll spy untouched.

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

// One flat pass over a scope -- the page content, or a single tab pane -- for
// the headings that belong to it and the tab groups it contains, in document
// order. Panes are not descended into: each is a scope of its own, so that an
// <h2> inside a tab stays under its tab instead of escaping to the page level.
function scanScope(root) {
    const nodes = [];

    function walk(node) {
        for (const child of node.children) {
            if (child.matches(SKIP)) {
                continue;
            }

            if (child.classList.contains('tab-content')) {
                const panes = [...child.children].filter((pane) => pane.classList.contains('tab-pane') && pane.id);
                if (panes.length) {
                    nodes.push({ panes });
                }
                continue;
            }

            const heading = /^H([1-6])$/.exec(child.tagName);
            if (heading) {
                if (child.id) {
                    nodes.push({ heading: child, level: Number(heading[1]) });
                }
                continue;
            }

            if (child.firstElementChild) {
                walk(child);
            }
        }
    }

    walk(root);
    return nodes;
}

// Outline of one scope. Headings nest by their level, and a tab group nests its
// panes one level below the heading it follows.
//
// `startLevel` anchors the depth budget. The page scope passes START_LEVEL so
// that it matches the server-rendered TOC, but a tab has no such fixed level: a
// pane whose sections are <h4> is as much a top level as a page whose sections
// are <h2>. Its shallowest heading therefore sets the anchor, and the scope
// keeps the same span of levels the page gets.
function buildOutline(root, startLevel) {
    const nodes = scanScope(root);
    const levels = nodes.filter((node) => node.heading).map((node) => node.level);
    const start = startLevel ?? (levels.length ? Math.min(...levels) : START_LEVEL);
    const end = start + END_LEVEL - START_LEVEL;

    const items = [];
    const stack = [{ level: start - 1, children: items }];

    function add(item, level) {
        while (stack.length > 1 && stack[stack.length - 1].level >= level) {
            stack.pop();
        }
        stack[stack.length - 1].children.push(item);
        stack.push({ level, children: item.children });
    }

    for (const node of nodes) {
        if (node.panes) {
            // Fixed for the whole group so that the panes end up as siblings.
            const level = stack[stack.length - 1].level + 1;
            for (const pane of node.panes) {
                add({ id: pane.id, text: paneLabel(pane), tab: true, children: buildOutline(pane) }, level);
            }
            continue;
        }

        if (node.level >= start && node.level <= end) {
            add({ id: node.heading.id, text: headingText(node.heading), children: [] }, node.level);
        }
    }

    return items;
}

// Mirrors the markup of Hugo's .TableOfContents (markup.tableOfContents.ordered
// is false) so that the existing TOC styles keep working. A tab that has an
// outline of its own gets a disclosure button in front of its label; `prefix`
// keeps the ids it needs unique between the desktop and the mobile TOC.
function renderOutline(items, prefix) {
    const list = document.createElement('ul');
    for (const item of items) {
        const entry = document.createElement('li');
        const link = document.createElement('a');
        link.href = `#${item.id}`;
        link.textContent = item.text;

        if (!item.tab || !item.children.length) {
            entry.append(link);
            if (item.children.length) {
                entry.append(renderOutline(item.children, prefix));
            }
            list.append(entry);
            continue;
        }

        const subtree = renderOutline(item.children, prefix);
        subtree.id = `${prefix}-outline-${item.id}`;
        link.id = `${prefix}-label-${item.id}`;

        // Labelled by the tab's own link, so the button needs no separate wording.
        const toggle = document.createElement('button');
        toggle.type = 'button';
        toggle.className = 'toc-tab-toggle';
        toggle.setAttribute('aria-controls', subtree.id);
        toggle.setAttribute('aria-labelledby', link.id);

        const head = document.createElement('div');
        head.className = 'toc-tab';
        head.append(toggle, link);

        entry.dataset.tocPane = item.id;
        entry.append(head, subtree);
        list.append(entry);
    }
    return list;
}

function setExpanded(entry, expanded) {
    entry.querySelector(':scope > .toc-tab > .toc-tab-toggle')
        .setAttribute('aria-expanded', String(expanded));
    entry.querySelector(':scope > ul').hidden = !expanded;
}

// Expand the outline of every shown pane and collapse the rest, so that the TOC
// covers about as much as the page does. A manual toggle holds until the next
// tab switch.
function syncTabOutlines() {
    for (const entry of document.querySelectorAll('.page-links [data-toc-pane]')) {
        const pane = document.getElementById(entry.dataset.tocPane);
        setExpanded(entry, Boolean(pane?.classList.contains('active')));
    }
}

function toggleTabOutline(event) {
    const toggle = event.target.closest('.page-links .toc-tab-toggle');
    if (!toggle) {
        return;
    }
    setExpanded(toggle.closest('[data-toc-pane]'), toggle.getAttribute('aria-expanded') !== 'true');
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
            if (!heading.getClientRects().length) {
                continue;
            }
            if (heading.getBoundingClientRect().top > ACTIVATION_OFFSET) {
                // Nothing reached yet: keep the first entry highlighted.
                current ??= heading;
                break;
            }
            current = heading;
        }

        if (current === active) {
            return;
        }
        for (const link of links.get(active?.id) ?? []) {
            link.classList.remove('active');
        }
        for (const link of links.get(current?.id) ?? []) {
            link.classList.add('active');
        }
        active = current;
    }

    function schedule() {
        if (queued) {
            return;
        }
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
    if (!content || !content.querySelector('.tab-content')) {
        return;
    }

    const outline = buildOutline(content, START_LEVEL);
    if (!outline.length) {
        return;
    }

    // Desktop (#toc) and mobile (#TableOfContents) both render into .page-links.
    const navs = document.querySelectorAll('.page-links > nav');
    if (!navs.length) {
        return;
    }

    const links = new Map();
    for (const nav of navs) {
        nav.replaceChildren(renderOutline(outline, nav.id));
        for (const link of nav.querySelectorAll('a')) {
            const id = decodeURIComponent(link.hash.slice(1));
            links.set(id, [...(links.get(id) ?? []), link]);
        }
    }

    syncTabOutlines();
    document.addEventListener('shown.bs.tab', syncTabOutlines);
    document.addEventListener('click', toggleTabOutline);

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
