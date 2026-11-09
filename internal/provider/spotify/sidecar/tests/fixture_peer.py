"""Synthetic source pages, routed through the real framed transport offline."""
import base64
import copy
import io
import json
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

from protocol import BridgeTransport, envelope, read_frame, write_frame

FIXTURE = Path(__file__).with_name("fixtures") / "playlists.json"


class FixturePeer:
    def __init__(self, case="large", *, status=200, fill=False):
        fixture = json.loads(FIXTURE.read_text(encoding="utf-8"))
        self.ref, self.name = fixture["playlistRef"], fixture["name"]
        self.template = fixture["trackTemplate"]
        self.case = fixture["cases"][case]
        self.status, self.fill = status, fill
        self.calls = []
        self.writer, self.reader = io.BytesIO(), io.BytesIO()
        self.transport = BridgeTransport(self, self.writer, "fixture")

    def read(self, size):
        if self.reader.tell() == len(self.reader.getvalue()):
            self.writer.seek(0)
            request = read_frame(self.writer)
            self.writer = self.transport.writer = io.BytesIO()
            self.calls.append(request)
            body = self.reply(request["url"])
            payload = body if isinstance(body, str) else json.dumps(body)
            self.reader = io.BytesIO()
            write_frame(self.reader, envelope("http.response", "fixture", sequence=request["sequence"],
                        status=self.status, headers={}, body=base64.b64encode(payload.encode()).decode()))
            self.reader.seek(0)
        return self.reader.read(size)

    def reply(self, url):
        parsed = urlsplit(url)
        if parsed.netloc == "open.spotify.com" and parsed.path == f"/embed/playlist/{self.ref}":
            data = {"props": {"pageProps": {"state": {
                "data": {"entity": {"uri": f"spotify:playlist:{self.ref}", "id": self.ref,
                                     "name": self.name, "trackList": []}},
                "settings": {"session": {"accessToken": "offline-not-a-token",
                             "accessTokenExpirationTimestampMs": 9999999999999, "isAnonymous": True}},
            }}}}
            return '<script id="__NEXT_DATA__" type="application/json">' + json.dumps(data) + '</script>'
        assert parsed.netloc == "api-partner.spotify.com"
        query = parse_qs(parsed.query)
        variables = json.loads(query["variables"][0])
        if query["operationName"] == ["getTrack"]:
            raw = copy.deepcopy(self.template) if self.fill else {"uri": variables["uri"]}
            raw["uri"] = variables["uri"]
            raw["duration"] = raw.pop("trackDuration", None)
            artists = raw.pop("artists", {}).get("items")
            if artists is not None:
                raw["firstArtist"], raw["otherArtists"] = {"items": artists[:1]}, {"items": artists[1:]}
            return {"data": {"trackUnion": raw}}
        assert query["operationName"] == ["fetchPlaylist"]
        assert variables["uri"] == f"spotify:playlist:{self.ref}"
        if self.case.get("queryError"):
            return {"errors": [{"message": "PersistedQueryNotFound"}]}
        offset, total = variables["offset"], self.case["count"]
        if self.case.get("repeatPage") and offset:
            offset = 0
        items = []
        for i in range(offset, min(offset + variables["limit"], total)):
            if i >= self.case.get("truncateAt", total):
                break
            raw = copy.deepcopy(self.template)
            number = self.case.get("refs", list(range(1, total + 1)))[i]
            raw["uri"] = f"spotify:track:{number:022d}"
            raw["__typename"] = self.case.get("types", {}).get(str(i), "Track")
            for field in self.case.get("missing", []):
                raw.pop(field, None)
            items.append({"itemV2": {"data": raw}})
        if offset >= 100:
            total += self.case.get("totalDeltaAt100", 0)
        return {"data": {"playlistV2": {"uri": f"spotify:playlist:{self.ref}", "name": self.name,
                                      "content": {"totalCount": total, "items": items}}}}
