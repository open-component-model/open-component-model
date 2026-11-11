import { realpathSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";
import { DOMParser, XMLSerializer, onWarningStopParsing } from "@xmldom/xmldom";

const RELEASES_URL = "https://github.com/open-component-model/open-component-model/releases";
const TAG_URL = `${RELEASES_URL}/tag/`;
const FEED_URL = "https://ocm.software/releases.xml";
const CANONICAL_TAG = /^v[0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?$/;

function isCanonicalTag(tag) {
    return typeof tag === "string" && CANONICAL_TAG.exec(tag)?.[0] === tag;
}

function parseXml(xml) {
    return new DOMParser({ onError: onWarningStopParsing }).parseFromString(xml, "application/xml");
}

function children(element, name) {
    return Array.from(element.childNodes).filter((node) => node.nodeType === 1 && node.tagName === name);
}

function text(element, name) {
    return children(element, name)[0]?.textContent ?? "";
}

function publicationDate(value) {
    const match = typeof value === "string" && /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.exec(value);
    const date = new Date(value);
    if (!match || match[0] !== value || !Number.isFinite(date.getTime())) {
        throw new Error("Publication timestamp must be an ISO date with a timezone");
    }
    const [, year, month, day, hour, minute, second] = match.map(Number);
    // Date.parse normalizes invalid calendar dates instead of rejecting them.
    const daysInMonth = new Date(Date.UTC(year, month, 0)).getUTCDate();
    if (year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth || hour > 23 || minute > 59 || second > 59) {
        throw new Error("Invalid publication timestamp");
    }
    return date;
}

function newFeed() {
    return parseXml(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>OCM Releases</title>
    <link>${RELEASES_URL}</link>
    <description>Release candidates and final releases of Open Component Model.</description>
    <language>en</language>
    <atom:link href="${FEED_URL}" rel="self" type="application/rss+xml"/>
  </channel>
</rss>`);
}

function releaseItem(document, tag, published) {
    const url = `${TAG_URL}${tag}`;
    const item = document.createElement("item");
    for (const [name, value] of [
        ["title", `OCM ${tag} released`],
        ["link", url],
        ["guid", url],
        ["pubDate", published.toUTCString()],
        ["description", `OCM ${tag} is available. Release notes and downloads: ${url}`]
    ]) {
        item.appendChild(document.createTextNode("\n      "));
        const element = document.createElement(name);
        element.appendChild(document.createTextNode(value));
        if (name === "guid") {
            element.setAttribute("isPermaLink", "true");
        }
        item.appendChild(element);
    }
    item.appendChild(document.createTextNode("\n    "));
    return item;
}

function feedItems(document) {
    const root = document.documentElement;
    if (!root || root.tagName !== "rss" || root.namespaceURI || root.getAttribute("version") !== "2.0" || children(root, "channel").length !== 1) {
        throw new Error("Expected an RSS 2.0 feed with one channel");
    }
    const channel = children(root, "channel")[0];
    const items = new Map();
    for (const element of children(channel, "item")) {
        const guid = text(element, "guid");
        if (!guid.startsWith(TAG_URL) || !isCanonicalTag(guid.slice(TAG_URL.length))) {
            throw new Error("Feed item has no canonical release GUID");
        }
        if (items.has(guid) || text(element, "link") !== guid) {
            throw new Error("Feed has duplicate GUIDs or a mismatched release link");
        }
        const date = text(element, "pubDate");
        const publishedMs = Date.parse(date);
        if (!Number.isFinite(publishedMs) || !/(?:[A-Z]{1,5}|[+-]\d{4})$/i.test(date)) {
            throw new Error("Feed publication date must include a valid timezone");
        }
        items.set(guid, { element, guid, publishedMs });
    }
    return { channel, items };
}

async function updateReleaseFeed(release, feedPath, pendingPath) {
    if (!release || typeof release !== "object" || Array.isArray(release)) {
        throw new Error("Expected one GitHub release object");
    }
    if (!isCanonicalTag(release.tag_name)) {
        throw new Error("Expected a canonical RC or final release tag");
    }
    if (release.draft !== false || typeof release.published_at !== "string") {
        throw new Error("The release must be published, not a draft");
    }
    const published = publicationDate(release.published_at);
    let original;
    try {
        original = await readFile(feedPath, "utf8");
    } catch (error) {
        if (error.code !== "ENOENT") {
            throw error;
        }
    }
    const document = original === undefined ? newFeed() : parseXml(original);
    const { channel, items } = feedItems(document);
    let changed = false;
    if (pendingPath) {
        const pending = feedItems(parseXml(await readFile(pendingPath, "utf8")));
        // main is authoritative for published entries; retain pending-only ones.
        for (const [guid, entry] of pending.items) {
            if (!items.has(guid)) {
                items.set(guid, { ...entry, element: document.importNode(entry.element, true) });
                changed = true;
            }
        }
    }
    const guid = `${TAG_URL}${release.tag_name}`;
    if (!items.has(guid)) {
        items.set(guid, { guid, publishedMs: published.getTime(), element: releaseItem(document, release.tag_name, published) });
        changed = true;
    }
    if (!changed) {
        return false;
    }

    for (const node of Array.from(channel.childNodes)) {
        if ((node.nodeType === 1 && node.tagName === "item") || (node.nodeType === 3 && !node.textContent.trim())) {
            channel.removeChild(node);
        }
    }
    const ordered = [...items.values()].sort((a, b) => b.publishedMs - a.publishedMs || (a.guid < b.guid ? -1 : a.guid > b.guid ? 1 : 0));
    for (const { element } of ordered) {
        channel.appendChild(element);
    }
    for (const node of Array.from(channel.childNodes)) {
        channel.insertBefore(document.createTextNode("\n    "), node);
    }
    channel.appendChild(document.createTextNode("\n  "));
    const content = `${new XMLSerializer().serializeToString(document).trimEnd()}\n`;
    if (content === original) {
        return false;
    }
    await writeFile(feedPath, content, "utf8");
    return true;
}

async function main() {
    const { values } = parseArgs({
        options: {
            release: { type: "string" },
            feed: { type: "string" },
            pending: { type: "string" }
        },
        strict: true,
        allowPositionals: false
    });
    if (!values.release || !values.feed) {
        throw new Error("--release and --feed are required");
    }
    const release = JSON.parse(await readFile(values.release, "utf8"));
    const changed = await updateReleaseFeed(release, values.feed, values.pending);
    console.log(changed ? "Release feed updated." : "Release already present; feed unchanged.");
}

if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
    main().catch((error) => {
        console.error(`[ERROR] ${error.message}`);
        process.exitCode = 1;
    });
}
