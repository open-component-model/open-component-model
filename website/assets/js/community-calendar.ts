/*
 * Community calendar — entrypoint for the {{< community-calendar >}}
 * shortcode. Renders the LFX webcal feed using the same FullCalendar
 * dayGridMonth view LFX shows on
 * https://zoom-lfx.platform.linuxfoundation.org/.
 *
 * We use ical.js directly instead of @fullcalendar/icalendar because the
 * plugin drops the iCalendar UID, which we need for stable filtering of
 * retired meetings (see BLOCKED_MEETING_IDS).
 */

import {Calendar, type EventInput} from "@fullcalendar/core";
import dayGridPlugin from "@fullcalendar/daygrid";
import ICAL from "ical.js";

// Retired meetings that linger in the LFX feed. Filter by Zoom meeting ID
// (the UID prefix), which survives SUMMARY edits. Recurring events have
// two UID shapes — "<id>" and "<id>:<recurrence-date>" — split on ":".
// Drop entries once LFX cleans up the source.
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

// Fetch the iCal feed and expand recurring events into FullCalendar
// inputs, preserving UID in `id` for downstream filtering.
async function fetchEvents(feed: string, range: {start: Date; end: Date}): Promise<CommunityEvent[]> {
    const response = await fetch(feed, {method: "GET"});
    if (!response.ok) {
        throw new Error(`fetch ${feed}: ${response.status} ${response.statusText}`);
    }
    const text = await response.text();

    // Parse the feed. A malformed feed throws here; we surface a clear
    // error so the eventual FullCalendar failure UI has something
    // diagnosable in the console.
    let vcalendar: ICAL.Component;
    try {
        vcalendar = new ICAL.Component(ICAL.parse(text));
    } catch (cause) {
        console.error("community-calendar: failed to parse iCal feed", {feed, cause});
        const message = cause instanceof Error ? cause.message : String(cause);
        // Attach cause as a property — the Error(message, {cause}) form
        // is ES2022; this project's TS lib target is ES2020. Runtime
        // engines have supported the property for years and ESLint's
        // preserve-caught-error rule accepts this shape.
        const err = new Error(`parse ${feed}: ${message}`);
        (err as Error & {cause: unknown}).cause = cause;
        throw err;
    }

    const events: CommunityEvent[] = [];

    // Pad the expansion window ±1 day. RRULE expansion across timezones
    // and DST transitions can yield instances whose UTC timestamp lies
    // just outside the nominal range even though the user sees them as
    // "in" the month. The extra day absorbs that drift; FullCalendar's
    // own range filter discards the surplus before render. Mirrors what
    // the @fullcalendar/icalendar plugin does internally.
    const rangeStart = ICAL.Time.fromJSDate(addDays(range.start, -1), false);
    const rangeEnd = ICAL.Time.fromJSDate(addDays(range.end, 1), false);

    for (const vevent of vcalendar.getAllSubcomponents("vevent")) {
        // Per-event try/catch: one malformed VEVENT (bad DTSTART, broken
        // RRULE, etc.) shouldn't drop the whole calendar. Skip the bad
        // one, log it, render the rest.
        try {
            const ev = new ICAL.Event(vevent);
            if (isBlocked(ev.uid)) continue;

            if (ev.isRecurring()) {
                const iter = ev.iterator();
                let next: ICAL.Time | null;
                while ((next = iter.next()) && next.compare(rangeEnd) <= 0) {
                    if (next.compare(rangeStart) < 0) continue;
                    const o = ev.getOccurrenceDetails(next);
                    events.push(buildEvent(o.item, o.startDate, o.endDate));
                }
            } else if (!vevent.hasProperty("recurrence-id")) {
                // Skip top-level RECURRENCE-ID overrides; they're already
                // emitted by their master's getOccurrenceDetails.
                events.push(buildEvent(ev, ev.startDate, ev.endDate));
            }
        } catch (cause) {
            const uid = vevent.getFirstPropertyValue("uid");
            console.warn("community-calendar: skipping malformed VEVENT", {uid, cause});
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
        end: end ? end.toJSDate() : undefined,
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
        initialView: "dayGridMonth",
        firstDay: 1,           // Monday-first, matching Europe/Berlin
        weekends: false,       // no project meetings on weekends
        fixedWeekCount: false, // don't pad short months with neighbor days
        headerToolbar: {left: "prev,next today", center: "title", right: ""},
        height: "auto",
        aspectRatio: 2,        // flatten the grid (default 1.35 is too tall)
        dayMaxEventRows: 2,
        eventTimeFormat: {hour: "2-digit", minute: "2-digit", hour12: false},
        displayEventEnd: false,
        events: (info, success, failure) => {
            fetchEvents(feed, {start: info.start, end: info.end}).then(success).catch(failure);
        },
        eventClick(info) {
            const url = extractJoinUrl(info.event.extendedProps.description as string | undefined);
            if (!url) return;
            const e = info.jsEvent;
            // Let modifier-clicks fall through to default browser behavior.
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
            e.preventDefault();
            window.open(url, "_blank", "noopener");
        },
        eventDidMount(info) {
            const d = info.event.extendedProps.description as string | undefined;
            if (!d) return;
            const trimmed = d.replace(/\s+/g, " ").trim();
            const el = info.el as HTMLElement;
            el.title = trimmed.length > 200 ? trimmed.slice(0, 200) + "…" : trimmed;
            if (extractJoinUrl(d)) el.style.cursor = "pointer";
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
