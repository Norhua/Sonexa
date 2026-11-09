"""Version-pinned SpotifyScraper adapter; no HTTP implementation lives here."""
from __future__ import annotations

import copy
import hashlib
import json
import re
from collections import Counter
from datetime import datetime, timezone
from importlib.metadata import version

from spotify_scraper import SpotifyClient
from spotify_scraper.api import parse_embed, parse_entities
from spotify_scraper.errors import SpotifyScraperError

SCRAPER_VERSION = "3.9.2"
MAX_ITEMS = 20_000
MAX_PAGES = 200
PAGE_SIZE = 100
ID = re.compile(r"[A-Za-z0-9]{22}\Z")
EXCLUDED_TYPES = {"Episode", "LocalTrack"}


class ReadError(Exception):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code


def text(value):
    return value if isinstance(value, str) else None


def duration(value):
    return value if type(value) is int and value >= 0 else None


def mapping(value):
    return value if isinstance(value, dict) else {}


def track_id(uri):
    if not isinstance(uri, str) or not uri.startswith("spotify:track:"):
        raise ReadError("incomplete", "歌曲身份缺失或不可靠")
    ref = uri.removeprefix("spotify:track:")
    if not ID.fullmatch(ref):
        raise ReadError("incomplete", "歌曲身份缺失或不可靠")
    return ref


def parse_track(data):
    """Use the library parser with a local, non-mutating missing-field shim.

    The pinned model requires name/album strings and converts missing duration
    to zero. Empty strings exist ONLY in the parser copy, then original presence
    is restored in the projection. No global monkey patch or invented identity.
    """
    ref = track_id(data.get("uri"))
    node = copy.deepcopy(data)
    title = text(data.get("name"))
    album = mapping(data.get("albumOfTrack"))
    album_name = text(album.get("name"))
    milliseconds = duration(mapping(data.get("trackDuration")).get("totalMilliseconds"))
    artist_nodes = mapping(data.get("artists")).get("items")
    artists_known = isinstance(artist_nodes, list) and all(
        isinstance(a, dict) and text(mapping(a.get("profile")).get("name")) is not None
        for a in artist_nodes
    )
    node["name"] = title if title is not None else ""
    if album:
        node["albumOfTrack"]["name"] = album_name if album_name is not None else ""
        node["albumOfTrack"]["uri"] = text(album.get("uri")) or ""
    else:
        node.pop("albumOfTrack", None)
    if not artists_known:
        node.pop("artists", None)
    parsed = parse_entities.parse_playlist_tracks_page(
        {"content": {"items": [{"itemV2": {"data": node}}]}}
    )
    if len(parsed) != 1 or parsed[0].track.id != ref:
        raise ReadError("incomplete", "歌曲解析数量或身份不一致")
    track = parsed[0].track
    return {
        "trackRef": ref,
        "title": track.name if title is not None else None,
        "artists": [a.name for a in track.artists] if artists_known else None,
        "album": track.album.name if track.album and album_name is not None else None,
        "durationMS": track.duration_ms if milliseconds is not None else None,
    }


def fill_missing(client, session, entry):
    """One bounded detail read of the SAME identity, not the facade fallback."""
    if all(entry[key] is not None for key in ("title", "artists", "album", "durationMS")):
        return
    raw = client._fetch_union("track", entry["trackRef"], "trackUnion", session)
    if track_id(raw.get("uri")) != entry["trackRef"] or raw.get("id", entry["trackRef"]) != entry["trackRef"]:
        raise ReadError("incomplete", "详情返回了不同歌曲身份")
    node = dict(raw)
    node["__typename"] = "Track"
    node["trackDuration"] = raw.get("duration")
    # Pinned detail schema splits the ordered credit into first/other artists.
    first = mapping(raw.get("firstArtist")).get("items")
    other = mapping(raw.get("otherArtists")).get("items")
    if isinstance(first, list) and isinstance(other, list):
        node["artists"] = {"items": first + other}
    detail = parse_track(node)
    for key in ("title", "artists", "album", "durationMS"):
        if entry[key] is None:
            entry[key] = detail[key]


def read_playlist(playlist_ref, transport):
    if version("SpotifyScraper") != SCRAPER_VERSION:
        raise ReadError("runtime_unavailable", "SpotifyScraper 版本不匹配")
    if not isinstance(playlist_ref, str) or not ID.fullmatch(playlist_ref):
        raise ReadError("input", "Spotify 歌单 ID 无效")
    try:
        with SpotifyClient(transport=transport) as client:
            return _read(client, playlist_ref)
    except ReadError:
        raise
    except (SpotifyScraperError, ValueError, TypeError, KeyError):
        # Do not leak a token, raw URL, response body, or library diagnostics.
        raise ReadError("incomplete", "无法取得可核对的完整歌单") from None


def _read(client, ref):
    next_data = client._fetch_next_data("playlist", ref)
    entity = parse_embed.get_entity(next_data)
    if entity.get("uri") != f"spotify:playlist:{ref}":
        raise ReadError("incomplete", "歌单身份不一致")
    session = parse_embed.get_session(next_data)
    if not session.is_anonymous:
        raise ReadError("access", "只接受匿名公开歌单会话")
    entries, excluded, seen_pages = [], [], set()
    occurrences = Counter()
    raw_seen, declared_total, name = 0, None, None
    for page_number in range(1, MAX_PAGES + 1):
        union = client._fetch_union(
            "playlist", ref, "playlistV2", session,
            overrides={"offset": raw_seen, "limit": PAGE_SIZE},
        )
        if union.get("uri") != f"spotify:playlist:{ref}" or not isinstance(union.get("name"), str):
            raise ReadError("incomplete", "分页歌单身份或名称缺失")
        content = mapping(union.get("content"))
        total, items = content.get("totalCount"), content.get("items")
        if type(total) is not int or total < 0 or not isinstance(items, list):
            raise ReadError("incomplete", "分页缺少总数或原始条目")
        if total > MAX_ITEMS:
            raise ReadError("resource", "歌单超过条目上限")
        if declared_total is None:
            declared_total, name = total, union["name"]
        elif total != declared_total or union["name"] != name:
            raise ReadError("incomplete", "读取期间歌单发生变化")
        if len(items) > PAGE_SIZE or raw_seen + len(items) > total or (not items and raw_seen < total):
            raise ReadError("incomplete", "原始分页覆盖不完整")
        page_hash = hashlib.sha256(json.dumps(items, sort_keys=True, separators=(",", ":")).encode()).digest()
        if items and page_hash in seen_pages:
            raise ReadError("incomplete", "上游返回重复页面")
        seen_pages.add(page_hash)
        for offset, item in enumerate(items):
            data = mapping(mapping(mapping(item).get("itemV2")).get("data"))
            kind = data.get("__typename")
            position = raw_seen + offset + 1
            if kind in EXCLUDED_TYPES:
                excluded.append({"position": position, "kind": kind})
                continue
            if kind != "Track":
                raise ReadError("incomplete", "原始位置含未知或不可解释的条目")
            entry = parse_track(data)
            fill_missing(client, session, entry)
            occurrences[entry["trackRef"]] += 1
            entry.update(entryKey=f'{entry["trackRef"]}:{occurrences[entry["trackRef"]]}', position=position)
            entries.append(entry)
        raw_seen += len(items)
        if raw_seen == declared_total:
            return {
                "playlistRef": ref, "name": name,
                "fetchedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
                "entries": entries, "excluded": excluded,
                "completeness": {"complete": True, "rawSeen": raw_seen,
                                 "declaredTotal": declared_total, "pages": page_number,
                                 "trackCount": len(entries), "excludedCount": len(excluded)},
            }
    raise ReadError("resource", "歌单超过分页上限")
