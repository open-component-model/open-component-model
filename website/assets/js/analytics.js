// Plausible analytics wiring for ocm.software, aligned with gardener.cloud.
// Cookieless: no per-user session chaining, no persistent identifiers, no PII.
// No 404 tracking: the site has no 404 page, GitHub Pages serves its own without our script.
import { init } from "@plausible-analytics/tracker";

/**
 * Report each page under the URL the site links to (Hugo builds every page as `/foo/index.html`,
 * linked as `/foo/`), so `/foo/` and `/foo/index.html` do not split metrics.
 * `/foo` needs no handling: GitHub Pages redirects it to `/foo/` before any JS runs.
 */
function normalizeEventUrl(rawUrl) {
    try {
        const parsed = new URL(rawUrl);
        parsed.pathname = parsed.pathname.replace(/\/index\.html$/, "/");
        return parsed.toString();
    } catch {
        return rawUrl;
    }
}

export function initAnalytics() {
    // Skip Netlify deploy previews and local/fork hosts so only live ocm.software sends events.
    if (location.hostname !== "ocm.software") {
        return;
    }
    init({
        domain: "ocm.software",
        // GitHub Pages has no proxy, so events go to the default https://plausible.io/api/event
        outboundLinks: true,
        transformRequest: (payload) => ({
            ...payload,
            u: normalizeEventUrl(payload.u)
        })
    });
}
