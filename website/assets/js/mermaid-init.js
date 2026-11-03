/*
 * Mermaid initialization.
 * -----------------------
 * Bundled by Hugo's esbuild wrapper (js.Build) at site-build time - the
 * `mermaid` bare specifier is resolved from node_modules/ during `hugo`.
 * No runtime call to cdn.jsdelivr.net (GDPR).
 *
 * Emitted from layouts/_default/baseof.html; kept off the doks-core
 * footer/esbuild partial on purpose so no project-level override of
 * upstream is needed (and no drift risk on doks-core bumps).
 */

import mermaid from "mermaid";

// Distinguish per-diagram syntax errors (expected, per-element) from systemic
// failures (unexpected: bad config, DOM exception, esbuild corruption). Both
// land in console.error so a single broken diagram doesn't break the page, but
// the message differs so systemic failures are not mistaken for one bad diagram.
const logMermaidError = (prefix) => (err) => {
    const isSyntax =
        err?.name === "UnknownDiagramError" ||
    err?.message?.includes("No diagram type detected") ||
    err?.message?.includes("Parse error");
    if (isSyntax) {
        console.error(`${prefix} (diagram syntax):`, err);
    } else {
        console.error(`${prefix} (unexpected — check initialization):`, err);
    }
};

// Why not `startOnLoad: true`:
//   startOnLoad wires a single load / DOMContentLoaded listener at the
//   moment initialize() is called. `type="module"` scripts are deferred,
//   so the event may have already fired by the time this runs, leaving
//   .mermaid blocks as raw text. Drive run() ourselves and gate on
//   document.readyState instead.
const start = () => {
    const elements = Array.from(document.querySelectorAll(".mermaid"));
    if (!elements.length) {
        return;
    }

    // Cache the diagram source on each element BEFORE the first render, so a
    // subsequent theme-change re-render can restore the pre-SVG text. Once
    // mermaid processes an element, its textContent becomes the rendered SVG
    // and the original source is lost.
    elements.forEach((el) => {
        el.setAttribute("data-mermaid-source", el.textContent);
    });

    const getTheme = () => {
        const t = document.documentElement.getAttribute("data-bs-theme");
        if (t === "dark") {
            return "dark";
        }
        if (t === "light") {
            return "default";
        }
        // "auto" - fall back to the OS-level preference so the initial render
        // matches what doks-core's theme toggle would resolve to.
        return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "default";
    };

    // Mermaid measures text to lay out a diagram, and an element that is not
    // displayed (an inactive tab pane, a closed <details> in some browsers)
    // measures as zero: the diagram renders as an empty 16x16 SVG. Render only
    // what is visible, and render the rest once it becomes visible. Unrendered
    // elements keep their source text, so they are picked up whenever they
    // become visible.
    const renderVisible = (prefix) => {
        const nodes = Array.from(document.querySelectorAll(".mermaid:not([data-processed])")).filter(
            (el) => el.getClientRects().length > 0
        );
        if (!nodes.length) {
            return;
        }
        // mermaid.run() rejects on invalid diagram syntax; swallow the rejection
        // to a console.error so a single bad diagram doesn't surface as an
        // unhandled promise rejection on the page.
        mermaid.run({ nodes }).catch(logMermaidError(prefix));
    };

    const rerender = () => {
        document.querySelectorAll(".mermaid").forEach((el) => {
            const src = el.getAttribute("data-mermaid-source");
            if (src) {
                // Undo mermaid's "already rendered" guard and restore the source so
                // mermaid.run() picks the element up again with the new theme.
                el.removeAttribute("data-processed");
                el.textContent = src;
            }
        });
        mermaid.initialize({ theme: getTheme() });
        renderVisible("mermaid re-render failed");
    };

    mermaid.initialize({ startOnLoad: false, theme: getTheme() });
    renderVisible("mermaid render failed");
    // Bootstrap fires shown.bs.tab on the tab button after the pane is visible;
    // the event bubbles to the document.
    document.addEventListener("shown.bs.tab", () => renderVisible("mermaid render failed"));
    // A <details> fires toggle when it opens or closes. toggle does not bubble,
    // so listen in the capture phase.
    document.addEventListener(
        "toggle",
        (event) => {
            if (event.target instanceof HTMLDetailsElement && event.target.open) {
                renderVisible("mermaid render failed");
            }
        },
        true
    );

    // doks-core's setTheme() writes data-bs-theme AND dispatches 'themeChanged'
    // for every toggle, so a naive listener + observer pair re-renders twice
    // per switch. Coalesce both signals through a single requestAnimationFrame
    // handle - keeping both sources still catches third-party toggles that
    // bypass one path or the other (belt-and-suspenders), but only one render
    // happens per frame.
    let scheduled = 0;
    const scheduleRerender = () => {
        if (scheduled) {
            return;
        }
        scheduled = requestAnimationFrame(() => {
            scheduled = 0;
            rerender();
        });
    };

    document.addEventListener("themeChanged", scheduleRerender);

    // attributeFilter narrows the observation to the one attribute we care
    // about, so the callback isn't woken for unrelated <html> attribute writes
    // (lang, class, ...) and no per-record scan is needed.
    const observer = new MutationObserver(scheduleRerender);
    observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-bs-theme"]
    });
};

if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start, { once: true });
} else {
    start();
}
