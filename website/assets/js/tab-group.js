/* tab-group.js — tab-aware section + TOC visibility filter.
 *
 * DOM contract (produced by Hugo shortcodes):
 *   <div class="tab-group" data-tab-group="ID">
 *     <div class="tab-section" data-tab-name="NAME">…</div>
 *     …
 *   </div>
 *
 * Tab-groups may nest. A right-hand TOC nav (#TableOfContents or #toc) holds
 * <a href="#heading-id"> entries that may target headings inside tab-sections;
 * those entries are hidden when their containing section is not active.
 *
 * No external dependencies. Vanilla ES2020+. Idempotent on re-run.
 */
(function () {
  "use strict";

  var STORAGE_PREFIX = "tabGroup:";
  var TOC_SELECTOR = "nav#TableOfContents a, nav#toc a";

  /**
   * Read the persisted active section name for a group, falling back to the
   * first direct-child section's name when nothing is saved or the saved
   * name no longer exists.
   */
  function resolveActiveName(group, sections) {
    var groupId = group.dataset.tabGroup;
    var saved;
    try {
      saved = window.localStorage.getItem(STORAGE_PREFIX + groupId);
    } catch {
      saved = null;
    }
    if (saved) {
      for (var i = 0; i < sections.length; i++) {
        if (sections[i].dataset.tabName === saved) {
          return saved;
        }
      }
    }
    var fallback = sections.length > 0 ? sections[0].dataset.tabName : null;
    if (fallback) {
      try {
        window.localStorage.setItem(STORAGE_PREFIX + groupId, fallback);
      } catch {
        /* private mode / storage disabled — best effort */
      }
    }
    return fallback;
  }

  /**
   * Build the button bar for a single .tab-group. No-op if buttons already
   * exist (idempotent re-run guard).
   */
  function buildButtonBar(group) {
    if (group.querySelector(":scope > .tab-group-buttons")) {
      return;
    }

    var groupId = group.dataset.tabGroup;
    var sections = group.querySelectorAll(":scope > .tab-section");
    if (sections.length === 0) {
      return;
    }

    var activeName = resolveActiveName(group, sections);

    var nav = document.createElement("nav");
    nav.className = "tab-group-buttons";
    nav.setAttribute("data-tab-group", groupId);
    nav.setAttribute("role", "tablist");

    for (var i = 0; i < sections.length; i++) {
      var section = sections[i];
      var name = section.dataset.tabName;
      var isActive = name === activeName;

      var btn = document.createElement("button");
      btn.type = "button";
      btn.setAttribute("data-tab-name", name);
      btn.setAttribute("role", "tab");
      btn.setAttribute("aria-selected", isActive ? "true" : "false");
      btn.textContent = name;
      if (isActive) {
        btn.classList.add("tab-button--active");
      }
      btn.addEventListener("click", onButtonClick);
      nav.appendChild(btn);

      section.classList.toggle("tab-section--active", isActive);
    }

    group.insertBefore(nav, group.firstChild);
  }

  /**
   * Promote section `name` inside `group`: toggle section + button classes,
   * persist to localStorage, dispatch `tabchanged`. Returns true on success,
   * false when no direct-child section with that name exists (in which case
   * nothing is toggled, persisted, or dispatched — important so a stale URL
   * like `?tab=demo:Bogus` doesn't pollute storage).
   *
   * Shared by tab-button clicks and URL deep-link activation.
   */
  function activateSection(group, name) {
    var sections = group.querySelectorAll(":scope > .tab-section");
    var match = false;
    for (var i = 0; i < sections.length; i++) {
      if (sections[i].dataset.tabName === name) {
        match = true;
        break;
      }
    }
    if (!match) return false;

    var groupId = group.dataset.tabGroup;

    for (var k = 0; k < sections.length; k++) {
      sections[k].classList.toggle(
        "tab-section--active",
        sections[k].dataset.tabName === name
      );
    }

    var buttons = group.querySelectorAll(
      ":scope > .tab-group-buttons button"
    );
    for (var j = 0; j < buttons.length; j++) {
      var b = buttons[j];
      var bActive = b.dataset.tabName === name;
      b.classList.toggle("tab-button--active", bActive);
      b.setAttribute("aria-selected", bActive ? "true" : "false");
    }

    try {
      window.localStorage.setItem(STORAGE_PREFIX + groupId, name);
    } catch {
      /* private mode / storage disabled — best effort */
    }

    group.dispatchEvent(
      new CustomEvent("tabchanged", {
        detail: { groupId: groupId, activeName: name },
        bubbles: true,
      })
    );

    return true;
  }

  /**
   * Click handler for a tab button. Thin wrapper around `activateSection`
   * that resolves the enclosing group from the click target.
   */
  function onButtonClick(event) {
    var btn = event.currentTarget;
    var nav = btn.closest(".tab-group-buttons");
    if (!nav) return;
    var group = nav.parentElement;
    if (!group || !group.classList.contains("tab-group")) return;
    activateSection(group, btn.dataset.tabName);
  }

  /**
   * Compute the chain of (groupId, sectionName) pairs from the page root
   * down to the heading targeted by a TOC anchor. Outermost-first, joined
   * with `|` so a nested anchor reads e.g. `outer:RSA|inner:PEM`.
   *
   * Returns "" when the heading is not inside any tab-section.
   */
  function computeTabPath(target) {
    var pairs = [];
    var node = target.parentElement;
    while (node) {
      if (
        node.classList &&
        node.classList.contains("tab-section") &&
        node.parentElement &&
        node.parentElement.classList.contains("tab-group")
      ) {
        var group = node.parentElement;
        var groupId = group.dataset.tabGroup;
        var name = node.dataset.tabName;
        if (groupId && name) {
          // Walking up = innermost first; unshift to keep outermost-first order.
          pairs.unshift(groupId + ":" + name);
        }
      }
      node = node.parentElement;
    }
    return pairs.join("|");
  }

  /**
   * Decide whether a TOC entry whose stored path is `path` is currently
   * visible. An empty path means the heading is outside any tab-section
   * and is always visible.
   *
   * Relies on `buildButtonBar` having seeded localStorage for every
   * tab-group on the page, so a missing key means the group doesn't
   * exist (treat as visible).
   */
  function pathIsVisible(path) {
    if (!path) return true;
    var pairs = path.split("|");
    for (var i = 0; i < pairs.length; i++) {
      var pair = pairs[i];
      var idx = pair.indexOf(":");
      if (idx < 0) continue;
      var groupId = pair.slice(0, idx);
      var name = pair.slice(idx + 1);

      var saved;
      try {
        saved = window.localStorage.getItem(STORAGE_PREFIX + groupId);
      } catch {
        saved = null;
      }
      if (saved && saved !== name) return false;
    }
    return true;
  }

  /**
   * Minimal CSS.escape shim — only the characters likely to appear in a
   * Hugo-generated tab-group id (which is itself slugified, so usually safe,
   * but quotes/backslashes deserve guarding).
   */
  function cssEscape(s) {
    if (typeof window.CSS !== "undefined" && typeof window.CSS.escape === "function") {
      return window.CSS.escape(s);
    }
    return String(s).replace(/(["\\])/g, "\\$1");
  }

  /**
   * Look up an element by id, trying the URL-decoded form first and
   * falling back to the raw form if decoding fails. Returns null when
   * no element matches.
   */
  function findElementById(id) {
    try {
      return document.getElementById(decodeURIComponent(id));
    } catch {
      return document.getElementById(id);
    }
  }

  /**
   * Re-evaluate visibility for every TOC anchor. Anchors store their tab
   * path in `data-toc-tab-path` on first run and reuse it thereafter.
   */
  function updateTocVisibility() {
    var anchors = document.querySelectorAll(TOC_SELECTOR);
    if (anchors.length === 0) return;

    for (var i = 0; i < anchors.length; i++) {
      var a = anchors[i];
      var li = a.parentElement;
      while (li && li.tagName !== "LI") {
        li = li.parentElement;
      }
      if (!li) continue;

      var path;
      if (a.dataset.tocTabPath !== undefined) {
        path = a.dataset.tocTabPath;
      } else {
        var href = a.getAttribute("href") || "";
        if (href.charAt(0) !== "#" || href.length < 2) {
          a.dataset.tocTabPath = "";
          path = "";
        } else {
          var id = href.slice(1);
          var target = findElementById(id);
          path = target ? computeTabPath(target) : "";
          a.dataset.tocTabPath = path;
        }
      }

      li.classList.toggle("toc-hidden", !pathIsVisible(path));
    }
  }

  /**
   * Read `?tab=group:section[|group:section…]` from the current URL and
   * activate each pair via `activateSection`. Then, if a `#fragment` is
   * present, scroll the matching element into view on the next animation
   * frame — manual scroll is needed because the target may have been
   * `display:none` at the moment the browser tried its native fragment
   * scroll.
   *
   * Idempotent: re-running with the same URL produces the same DOM and
   * localStorage state.
   */
  function applyUrlTabState() {
    var params;
    try {
      params = new URLSearchParams(window.location.search);
    } catch {
      return;
    }

    var raw = params.get("tab");
    if (raw) {
      var pairs = raw.split("|");
      for (var i = 0; i < pairs.length; i++) {
        var pair = pairs[i];
        var idx = pair.indexOf(":");
        if (idx < 1 || idx === pair.length - 1) continue;
        var groupId = pair.slice(0, idx);
        var name = pair.slice(idx + 1);
        var group = document.querySelector(
          '.tab-group[data-tab-group="' + cssEscape(groupId) + '"]'
        );
        if (group) activateSection(group, name);
      }
    }

    var hash = window.location.hash;
    if (hash && hash.length > 1) {
      var target = findElementById(hash.slice(1));
      if (target) {
        window.requestAnimationFrame(function () {
          target.scrollIntoView({ block: "start", behavior: "smooth" });
        });
      }
    }
  }

  /**
   * Click handler for in-page heading anchors (Doks renders these as
   * <a class="anchor" href="#heading-id"> next to every heading). When
   * the target heading lives inside one or more tab-sections, rewrite
   * the address bar to include the active tab path so the URL the
   * user copies is a working deep-link.
   *
   * Native browser scroll handles the actual scrolling; we only touch
   * the URL.
   */
  function onAnchorClick(event) {
    var a = event.target.closest && event.target.closest("a.anchor");
    if (!a) return;
    var href = a.getAttribute("href") || "";
    if (href.charAt(0) !== "#" || href.length < 2) return;
    var target = findElementById(href.slice(1));
    if (!target) return;
    var path = computeTabPath(target);
    if (!path) return;

    var url = new URL(window.location.href);
    url.searchParams.set("tab", path);
    url.hash = href;
    window.history.replaceState(null, "", url.toString());
  }

  function main() {
    var groups = document.querySelectorAll(".tab-group");
    for (var i = 0; i < groups.length; i++) {
      buildButtonBar(groups[i]);
    }

    applyUrlTabState();

    updateTocVisibility();

    if (!document.__tabGroupTocListenerAttached) {
      document.addEventListener("tabchanged", function () {
        updateTocVisibility();
      });
      document.addEventListener("click", onAnchorClick);
      document.__tabGroupTocListenerAttached = true;
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", main);
  } else {
    main();
  }
})();
