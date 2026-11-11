"""Add one GitHub release to an RSS feed, preserving published and pending entries."""

import argparse
import json
import re
import sys
from datetime import datetime, timezone
from email.utils import format_datetime, parsedate_to_datetime
from pathlib import Path
import xml.etree.ElementTree as ET

RELEASES_URL = "https://github.com/open-component-model/open-component-model/releases"
TAG_URL = RELEASES_URL + "/tag/"
FEED_URL = "https://ocm.software/releases.xml"
CANONICAL_TAG = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?")
ATOM = "http://www.w3.org/2005/Atom"
ET.register_namespace("atom", ATOM)


def release_item(release):
    if not isinstance(release, dict):
        raise ValueError("Expected one GitHub release object")
    tag = release.get("tag_name")
    if not isinstance(tag, str) or not CANONICAL_TAG.fullmatch(tag):
        raise ValueError("Expected a canonical RC or final release tag")
    if release.get("draft") is not False or not isinstance(release.get("published_at"), str):
        raise ValueError("The release must be published, not a draft")
    published = datetime.fromisoformat(release["published_at"].replace("Z", "+00:00"))
    if published.tzinfo is None:
        raise ValueError("Publication timestamp must include a timezone")
    url = TAG_URL + tag
    item = ET.Element("item")
    ET.SubElement(item, "title").text = f"OCM {tag} released"
    ET.SubElement(item, "link").text = url
    ET.SubElement(item, "guid", isPermaLink="true").text = url
    ET.SubElement(item, "pubDate").text = format_datetime(published.astimezone(timezone.utc), usegmt=True)
    ET.SubElement(item, "description").text = f"OCM {tag} is available. Release notes and downloads: {url}"
    return item


def new_feed():
    root = ET.Element("rss", version="2.0")
    channel = ET.SubElement(root, "channel")
    for name, value in (
        ("title", "OCM Releases"),
        ("link", RELEASES_URL),
        ("description", "Release candidates and final releases of Open Component Model."),
        ("language", "en"),
    ):
        ET.SubElement(channel, name).text = value
    ET.SubElement(channel, f"{{{ATOM}}}link", href=FEED_URL, rel="self", type="application/rss+xml")
    return root


def feed_items(root):
    if root.tag != "rss" or root.get("version") != "2.0" or len(root.findall("channel")) != 1:
        raise ValueError("Expected an RSS 2.0 feed with one channel")
    items = {}
    for item in root.find("channel").findall("item"):
        guid = item.findtext("guid", "")
        if not guid.startswith(TAG_URL) or not CANONICAL_TAG.fullmatch(guid[len(TAG_URL):]):
            raise ValueError("Feed item has no canonical release GUID")
        if guid in items or item.findtext("link") != guid:
            raise ValueError("Feed has duplicate GUIDs or a mismatched release link")
        published = parsedate_to_datetime(item.findtext("pubDate", ""))
        if published.tzinfo is None:
            raise ValueError("Feed publication date must include a timezone")
        items[guid] = item
    return items


def update_feed(release, feed_path, pending_path=None):
    item = release_item(release)
    try:
        original = feed_path.read_bytes()
    except FileNotFoundError:
        original = None
    root = ET.fromstring(original) if original is not None else new_feed()
    items = feed_items(root)
    changed = False
    if pending_path is not None:
        # main is authoritative for already published entries; retain pending-only ones.
        for guid, pending_item in feed_items(ET.parse(pending_path).getroot()).items():
            if guid not in items:
                items[guid] = pending_item
                changed = True
    guid = item.findtext("guid")
    if guid not in items:
        items[guid] = item
        changed = True
    if not changed:
        return False

    channel = root.find("channel")
    for previous in channel.findall("item"):
        channel.remove(previous)
    ordered = sorted(items.values(), key=lambda entry: entry.findtext("guid"))
    ordered.sort(key=lambda entry: parsedate_to_datetime(entry.findtext("pubDate")), reverse=True)
    channel.extend(ordered)
    ET.indent(root, space="  ")
    content = ET.tostring(root, encoding="utf-8", xml_declaration=True) + b"\n"
    if content == original:
        return False
    feed_path.write_bytes(content)
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release", type=Path, required=True, help="JSON for one published GitHub release")
    parser.add_argument("--feed", type=Path, required=True, help="Existing RSS feed to update or create")
    parser.add_argument("--pending", type=Path, help="Feed from the current open PR, if any")
    args = parser.parse_args()
    with args.release.open(encoding="utf-8") as source:
        release = json.load(source)
    changed = update_feed(release, args.feed, args.pending)
    print("Release feed updated." if changed else "Release already present; feed unchanged.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, ET.ParseError) as error:
        print(f"[ERROR] {error}", file=sys.stderr)
        sys.exit(1)
