// Local override of @thulite/doks-core assets/js/tabs.js
// (based on https://github.com/gohugoio/hugoDocs/blob/master/_vendor/github.com/gohugoio/gohugoioTheme/assets/js/tabs.js)
//
// When the user picks a tab, apply the choice to every tab group on the page
// with the same label (e.g. pick "Linux" once and all OS groups follow).
//
// Differences to the theme version:
// - No localStorage: the theme persisted the picked label and re-applied it
//   on every page load, which could silently switch tabs away from what the
//   URL anchor points at. Tab state is now derived from the page and the URL
//   only (deep links are resolved in custom.js).
// - The theme built unquoted attribute selectors, which throw a SyntaxError
//   as soon as a tab name contains spaces or special characters (e.g.
//   "Option A: Root CA → Leaf (simple)") — crashing the rest of the app
//   bundle. The selector is now quoted and escaped.
// - The theme removed the "active" classes from ALL tabs and panes on the
//   page and re-added them only to matches, blanking every tab group that
//   does not contain the selected label (e.g. nested tab groups). Delegating
//   to Bootstrap's Tab API switches each matching group correctly and leaves
//   the other groups alone.

import { Tab } from 'bootstrap';

function toggleTabs(event) {
  event.preventDefault();
  const targetKey = event.currentTarget.getAttribute('data-toggle-tab');
  if (!targetKey) return;

  const selectedTabs = document.querySelectorAll(`[data-toggle-tab="${CSS.escape(targetKey)}"]`);
  for (const tab of selectedTabs) {
    Tab.getOrCreateInstance(tab).show();
  }
}

for (const tab of document.querySelectorAll('[data-toggle-tab]')) {
  tab.addEventListener('click', toggleTabs);
}
