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
    var saved = null;
    try {
      saved = window.localStorage.getItem(STORAGE_PREFIX + groupId);
    } catch (e) {
      saved = null;
    }
    if (saved) {
      for (var i = 0; i < sections.length; i++) {
        if (sections[i].dataset.tabName === saved) {
          return saved;
        }
      }
    }
    return sections.length > 0 ? sections[0].dataset.tabName : null;
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
   * Click handler for a tab button. Promotes the clicked button + matching
   * section, persists the choice, and emits a `tabchanged` CustomEvent on the
   * group element so the TOC filter (and anyone else) can react.
   */
  function onButtonClick(event) {
    var btn = event.currentTarget;
    var nav = btn.closest(".tab-group-buttons");
    if (!nav) return;
    var group = nav.parentElement;
    if (!group || !group.classList.contains("tab-group")) return;

    var groupId = group.dataset.tabGroup;
    var name = btn.dataset.tabName;

    var sections = group.querySelectorAll(":scope > .tab-section");
    for (var i = 0; i < sections.length; i++) {
      sections[i].classList.toggle(
        "tab-section--active",
        sections[i].dataset.tabName === name
      );
    }

    var buttons = nav.querySelectorAll("button");
    for (var j = 0; j < buttons.length; j++) {
      var b = buttons[j];
      var bActive = b === btn;
      b.classList.toggle("tab-button--active", bActive);
      b.setAttribute("aria-selected", bActive ? "true" : "false");
    }

    try {
      window.localStorage.setItem(STORAGE_PREFIX + groupId, name);
    } catch (e) {
      /* private mode / storage disabled — best effort */
    }

    group.dispatchEvent(
      new CustomEvent("tabchanged", {
        detail: { groupId: groupId, activeName: name },
        bubbles: true,
      })
    );
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
   */
  function pathIsVisible(path, firstSectionCache) {
    if (!path) return true;
    var pairs = path.split("|");
    for (var i = 0; i < pairs.length; i++) {
      var pair = pairs[i];
      var idx = pair.indexOf(":");
      if (idx < 0) continue;
      var groupId = pair.slice(0, idx);
      var name = pair.slice(idx + 1);

      var saved = null;
      try {
        saved = window.localStorage.getItem(STORAGE_PREFIX + groupId);
      } catch (e) {
        saved = null;
      }

      if (saved) {
        if (saved !== name) return false;
      } else {
        var firstName = firstSectionCache[groupId];
        if (firstName === undefined) {
          var group = document.querySelector(
            '.tab-group[data-tab-group="' + cssEscape(groupId) + '"]'
          );
          if (group) {
            var firstSection = group.querySelector(
              ":scope > .tab-section"
            );
            firstName = firstSection ? firstSection.dataset.tabName : null;
          } else {
            firstName = null;
          }
          firstSectionCache[groupId] = firstName;
        }
        if (firstName !== name) return false;
      }
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
   * Re-evaluate visibility for every TOC anchor. Anchors store their tab
   * path in `data-toc-tab-path` on first run and reuse it thereafter.
   */
  function updateTocVisibility() {
    var anchors = document.querySelectorAll(TOC_SELECTOR);
    if (anchors.length === 0) return;

    var firstSectionCache = {};

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
          var target = null;
          try {
            target = document.getElementById(decodeURIComponent(id));
          } catch (e) {
            target = document.getElementById(id);
          }
          path = target ? computeTabPath(target) : "";
          a.dataset.tocTabPath = path;
        }
      }

      var visible = pathIsVisible(path, firstSectionCache);
      li.classList.toggle("toc-hidden", !visible);
    }
  }

  function main() {
    var groups = document.querySelectorAll(".tab-group");
    for (var i = 0; i < groups.length; i++) {
      buildButtonBar(groups[i]);
    }

    updateTocVisibility();

    if (!document.__tabGroupTocListenerAttached) {
      document.addEventListener("tabchanged", function () {
        updateTocVisibility();
      });
      document.__tabGroupTocListenerAttached = true;
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", main);
  } else {
    main();
  }
})();
