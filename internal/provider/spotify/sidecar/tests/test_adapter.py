import io
import json
import socket
import struct
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

from adapter import ReadError, parse_track, read_playlist
from pathlib import Path
from protocol import MAX_FRAME, BridgeTransport, envelope, read_frame, write_frame
from tests.fixture_peer import FixturePeer


class AdapterTests(unittest.TestCase):
    def setUp(self):
        self.socket_guard = patch.object(socket.socket, "connect", side_effect=AssertionError("hidden network"))
        self.dns_guard = patch.object(socket, "getaddrinfo", side_effect=AssertionError("hidden DNS"))
        self.client_guard = patch("spotify_scraper.http.transport.HttpxTransport", side_effect=AssertionError("default transport"))
        self.socket_guard.start()
        self.dns_guard.start()
        self.client_guard.start()
        self.addCleanup(self.socket_guard.stop)
        self.addCleanup(self.dns_guard.stop)
        self.addCleanup(self.client_guard.stop)

    def read(self, case="large", **kwargs):
        peer = FixturePeer(case, **kwargs)
        return read_playlist(peer.ref, peer.transport), peer

    def test_all_pages_use_raw_offsets(self):
        snapshot, peer = self.read()
        self.assertEqual(snapshot["completeness"], {"complete": True, "rawSeen": 205,
                         "declaredTotal": 205, "pages": 3, "trackCount": 205, "excludedCount": 0})
        offsets = [json.loads(parse_qs(urlsplit(c["url"]).query)["variables"][0])["offset"] for c in peer.calls[1:]]
        self.assertEqual(offsets, [0, 100, 200])
        self.assertEqual(snapshot["entries"][0]["durationMS"], 123456)
        self.assertEqual(snapshot["entries"][0]["artists"], ["第一署名", "第二署名"])

    def test_exclusions_account_for_every_position(self):
        snapshot, peer = self.read("excluded")
        self.assertEqual(snapshot["completeness"]["rawSeen"], 103)
        self.assertEqual(len(snapshot["entries"]), 101)
        self.assertEqual(snapshot["excluded"], [{"position": 2, "kind": "Episode"}, {"position": 101, "kind": "LocalTrack"}])
        self.assertEqual(snapshot["entries"][-1]["position"], 103)
        self.assertEqual(len(peer.calls), 3)

    def test_missing_fields_remain_null(self):
        snapshot, peer = self.read("missing")
        entry = snapshot["entries"][0]
        for key in ("title", "artists", "album", "durationMS"):
            self.assertIsNone(entry[key])
        self.assertEqual(len(peer.calls), 3)  # embed + page + same-track detail
        self.assertIn("getTrack", peer.calls[-1]["url"])

    def test_same_track_detail_fills_only_unknown_fields(self):
        snapshot, _ = self.read("missing", fill=True)
        entry = snapshot["entries"][0]
        self.assertEqual(entry["title"], "测试歌曲")
        self.assertEqual(entry["durationMS"], 123456)
        self.assertEqual(entry["album"], "测试专辑")
        self.assertEqual(entry["artists"], ["第一署名", "第二署名"])

    def test_duplicate_occurrences_are_not_deduplicated(self):
        snapshot, _ = self.read("duplicate")
        a, b, c = snapshot["entries"]
        self.assertEqual(a["trackRef"], c["trackRef"])
        self.assertEqual([e["entryKey"][-2:] for e in (a, b, c)], [":1", ":1", ":2"])

    def test_explicit_empty_is_complete(self):
        snapshot, _ = self.read("empty")
        self.assertEqual(snapshot["entries"], [])
        self.assertEqual(snapshot["completeness"]["declaredTotal"], 0)

    def test_incomplete_never_returns_a_snapshot(self):
        for case in ("truncated", "changedTotal", "repeatPage", "unknown", "missingIdentity", "embedFallback"):
            with self.subTest(case=case), self.assertRaises(ReadError) as error:
                self.read(case)
            self.assertEqual(error.exception.code, "incomplete")

    def test_auth_and_limits_do_not_refresh_token_or_retry(self):
        for status in (401, 403, 429, 500):
            peer = FixturePeer(status=status)
            with self.subTest(status=status), self.assertRaises(ReadError):
                read_playlist(peer.ref, peer.transport)
            self.assertEqual(len(peer.calls), 1)

    def test_invalid_id_does_not_request(self):
        peer = FixturePeer()
        with self.assertRaises(ReadError):
            read_playlist("x; touch /tmp/no", peer.transport)
        self.assertEqual(peer.calls, [])

    def test_wrong_detail_identity_fails(self):
        peer = FixturePeer("missing")
        original = peer.reply
        def reply(url):
            data = original(url)
            if "getTrack" in url:
                data["data"]["trackUnion"]["uri"] = "spotify:track:" + "9" * 22
            return data
        peer.reply = reply
        with self.assertRaises(ReadError):
            read_playlist(peer.ref, peer.transport)


class ProtocolTests(unittest.TestCase):
    def test_shared_go_python_protocol_fixture(self):
        fixture = json.loads((Path(__file__).parents[2] / "testdata" / "protocol.json").read_text())
        for case in fixture["cases"]:
            entry = parse_track(case["raw"])
            entry.update(entryKey=entry["trackRef"] + ":1", position=1)
            self.assertEqual(entry, case["entry"])
        for name in ("empty", "failure"):
            stream = io.BytesIO()
            write_frame(stream, fixture[name])
            stream.seek(0)
            self.assertEqual(read_frame(stream), fixture[name])

    def test_utf8_round_trip(self):
        stream = io.BytesIO()
        data = envelope("hello", "自检", name="带 空格")
        write_frame(stream, data)
        stream.seek(0)
        self.assertEqual(read_frame(stream), data)

    def test_bad_frames(self):
        for data in (b"", b"\0\0", struct.pack(">I", MAX_FRAME + 1), b"\0\0\0\x01[", b"\0\0\0\x01\xff", b"\0\0\0\x02[]"):
            with self.subTest(data=data), self.assertRaises(ReadError):
                read_frame(io.BytesIO(data))

    def test_wrong_request_id_sequence_or_protocol(self):
        for field, value in (("requestId", "other"), ("sequence", 2), ("protocol", True), ("kind", "result")):
            message = envelope("http.response", "test", sequence=1, status=200, headers={}, body="e30=")
            message[field] = value
            stream = io.BytesIO()
            write_frame(stream, message)
            stream.seek(0)
            transport = BridgeTransport(stream, io.BytesIO(), "test")
            with self.subTest(field=field), self.assertRaises(ReadError):
                transport.get("https://api-partner.spotify.com/pathfinder/v1/query")


if __name__ == "__main__":
    unittest.main()
