/*
 * Community calendar — entrypoint loaded by the {{< community-calendar >}}
 * Hugo shortcode. Renders the live OCM project calendar (LFX webcal feed)
 * using FullCalendar's dayGridMonth view, the same primitive LFX itself
 * uses on https://zoom-lfx.platform.linuxfoundation.org/. Their production
 * SPA bundle references @fullcalendar/daygrid (verified 2026-06), so
 * matching their visual is straightforward neutral styling on top of the
 * public --fc-* / .fc-daygrid-* hooks.
 *
 * Why a custom event source instead of @fullcalendar/icalendar:
 * The plugin's buildNonDateProps() drops the iCalendar UID — events come
 * out with only {title, url, extendedProps:{location, organizer,
 * description}}. That makes UID-based filtering (the right way to hide
 * retired meetings without title-string fragility) impossible. The plugin
 * is a thin wrapper around ical.js's IcalExpander; we use ical.js directly
 * instead, which is ~30 lines and gives us full VEVENT access including
 * UID.
 *
 * Why bundle these from npm via Hugo's asset pipeline (and not <script
 * src="https://cdn..."> tags):
 * - Renovate watches package.json automatically; pinned CDN URLs are
 *   invisible to the bot and silently age out.
 * - One same-origin asset replaces three cross-origin CDN requests, no
 *   SRI to maintain, no defer-ordering between scripts (esbuild fixes
 *   dependency order at bundle time).
 * - Tree-shaking drops unused FullCalendar views (list, timegrid) we
 *   never touch.
 */

import {Calendar, type EventInput} from "@fullcalendar/core";
import dayGridPlugin from "@fullcalendar/daygrid";
import ICAL from "ical.js";

// Meetings that linger in the upstream LFX feed after they've been retired
// get filtered out client-side by their Zoom meeting ID. That ID is the
// prefix of each VEVENT's UID (and is duplicated in X-MEETING-ID and the
// join URL), so it's a stable identifier even if the SUMMARY is renamed.
// Recurring events produce two UID shapes — master ("<id>") and modified
// occurrences ("<id>:<recurrence-date>") — splitting on ":" normalizes
// both. Drop an entry once LFX cleans up the source feed.
const BLOCKED_MEETING_IDS = new Set<string>([
    "93093370350", // OCM Daily Stand-Up — meeting retired, feed entry stale
]);

function meetingId(uid: string): string {
    return uid.split(":")[0];
}

function isBlocked(uid: string): boolean {
    return BLOCKED_MEETING_IDS.has(meetingId(uid));
}

interface CommunityEvent extends EventInput {
    id: string;
    extendedProps: {
        uid: string;
        meetingId: string;
        description: string;
        location: string;
    };
}

/**
 * Fetch the iCal feed and expand recurring events into FullCalendar
 * EventInput[]. Each event carries the iCalendar UID in `id` so callers
 * can filter by stable identity rather than fragile title matching.
 *
 * Mirrors what @fullcalendar/icalendar does internally (IcalExpander +
 * date stringification) but preserves UID, which the plugin drops.
 */
async function fetchEvents(feed: string, range: {start: Date; end: Date}): Promise<CommunityEvent[]> {
    const response = await fetch(feed, {method: "GET"});
    if (!response.ok) {
        throw new Error(`fetch ${feed}: ${response.status} ${response.statusText}`);
    }
    const text = await response.text();
    const jcal = ICAL.parse(text);
    const vcalendar = new ICAL.Component(jcal);
    const events: CommunityEvent[] = [];

    // Walk every VEVENT. Recurring events have RRULE (master) or
    // RECURRENCE-ID (modified occurrence override).
    const vevents = vcalendar.getAllSubcomponents("vevent");

    // Pad the range — RRULE expansion can produce instances slightly
    // outside our nominal window when DTSTART is on a boundary day.
    const rangeStart = ICAL.Time.fromJSDate(addDays(range.start, -1), false);
    const rangeEnd = ICAL.Time.fromJSDate(addDays(range.end, 1), false);

    for (const vevent of vevents) {
        const ev = new ICAL.Event(vevent);
        if (isBlocked(ev.uid)) continue;

        if (ev.isRecurring()) {
            const iter = ev.iterator();
            let next: ICAL.Time | null;
            while ((next = iter.next()) && next.compare(rangeEnd) <= 0) {
                if (next.compare(rangeStart) < 0) continue;
                const occurrence = ev.getOccurrenceDetails(next);
                events.push(buildEvent(occurrence.item, occurrence.startDate, occurrence.endDate));
            }
        } else {
            // Skip modified-occurrence overrides at the top level — they
            // were already emitted by the master's getOccurrenceDetails.
            if (vevent.hasProperty("recurrence-id")) continue;
            events.push(buildEvent(ev, ev.startDate, ev.endDate));
        }
    }

    return events;
}

function buildEvent(ev: ICAL.Event, start: ICAL.Time, end: ICAL.Time | null): CommunityEvent {
    const id = meetingId(ev.uid);
    return {
        id,
        title: ev.summary,
        start: start.toJSDate(),
        end: end ? end.toJSDate() : null,
        url: extractEventUrl(ev) || undefined,
        extendedProps: {
            uid: ev.uid,
            meetingId: id,
            description: ev.description || "",
            location: ev.location || "",
        },
    };
}

function extractEventUrl(ev: ICAL.Event): string {
    const urlProp = ev.component.getFirstProperty("url");
    return urlProp ? String(urlProp.getFirstValue() ?? "") : "";
}

function addDays(d: Date, n: number): Date {
    const r = new Date(d);
    r.setDate(r.getDate() + n);
    return r;
}

function init(): void {
    const root = document.querySelector<HTMLElement>(".ocm-calendar");
    if (!root || root.dataset.rendered === "1") return;
    root.dataset.rendered = "1";

    const mount = root.querySelector<HTMLElement>(".ocm-calendar-mount");
    const loading = root.querySelector<HTMLElement>(".ocm-calendar-loading");
    if (!mount) return;
    mount.removeAttribute("aria-hidden");
    loading?.remove();

    const feed = root.dataset.feed;
    if (!feed) return;

    const calendar = new Calendar(mount, {
        plugins: [dayGridPlugin],
        // Month grid — the same default view LFX shows on
        // https://zoom-lfx.platform.linuxfoundation.org/.
        initialView: "dayGridMonth",
        // Week starts Monday — matches the European cadence of the
        // project's working week and how LFX renders for Europe/Berlin.
        firstDay: 1,
        // Hide Sat/Sun: no project meetings happen on weekends, and a
        // five-column grid gives the remaining days more horizontal room.
        weekends: false,
        // Render only the weeks that belong to the displayed month.
        // FullCalendar's default is a fixed 6-row grid (42 cells), which
        // pads short months with leading/trailing days from neighboring
        // months — visually distracting and not what LFX shows.
        fixedWeekCount: false,
        headerToolbar: {
            left: "prev,next today",
            center: "title",
            right: "",
        },
        height: "auto",
        // Flatten the grid: default aspectRatio is 1.35 (wider than tall),
        // which makes the month feel oversized inside a content page. 2.0
        // gives the calendar roughly half its default height while keeping
        // chips readable.
        aspectRatio: 2,
        // Cap stacked events per cell; FullCalendar adds a "+N more" link
        // that opens the daily popover.
        dayMaxEventRows: 2,
        eventTimeFormat: {hour: "2-digit", minute: "2-digit", hour12: false},
        displayEventEnd: false,
        events: (info, successCallback, failureCallback) => {
            fetchEvents(feed, {start: info.start, end: info.end})
                .then(successCallback)
                .catch(failureCallback);
        },
        // Click an event → open its Zoom join URL in a new tab. LFX puts
        // join URLs in DESCRIPTION; we extract the first one.
        eventClick(info) {
            const url = extractJoinUrl(info.event.extendedProps.description as string | undefined);
            if (!url) return;
            const e = info.jsEvent;
            // Only intercept plain clicks; modifier-clicks fall through
            // to default browser behavior.
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
            e.preventDefault();
            window.open(url, "_blank", "noopener");
        },
        // Hover tooltip with truncated description + clickable cursor
        // when a join URL is present.
        eventDidMount(info) {
            const d = info.event.extendedProps.description as string | undefined;
            if (!d) return;
            const trimmed = d.replace(/\s+/g, " ").trim();
            const el = info.el as HTMLElement;
            el.title = trimmed.length > 200 ? trimmed.slice(0, 200) + "…" : trimmed;
            if (extractJoinUrl(d)) {
                el.style.cursor = "pointer";
            }
        },
    });
    calendar.render();
}

function extractJoinUrl(description: string | undefined): string | null {
    if (!description) return null;
    let m = description.match(/https:\/\/zoom-lfx\.platform\.linuxfoundation\.org\/meeting\/[^\s<>"']+/);
    if (!m) m = description.match(/https:\/\/[a-z0-9-]+\.zoom\.us\/j\/[0-9]+[^\s<>"']*/i);
    return m ? m[0] : null;
}

if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
} else {
    init();
}
