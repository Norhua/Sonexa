"""Length-prefixed JSON and the only transport exposed to SpotifyScraper."""
from __future__ import annotations

import base64
import json
import struct

from adapter import ReadError

PROTOCOL = 1
MAX_FRAME = 32 * 1024 * 1024
MAX_RESPONSE = 8 * 1024 * 1024
MAX_RESPONSE_TOTAL = 256 * 1024 * 1024
MAX_SENDS = 500


def read_exact(stream, size):
    chunks = bytearray()
    while len(chunks) < size:
        chunk = stream.read(size - len(chunks))
        if not chunk:
            raise ReadError("protocol", "辅助程序通信提前结束")
        chunks.extend(chunk)
    return bytes(chunks)


def read_frame(stream):
    size = struct.unpack(">I", read_exact(stream, 4))[0]
    if not 0 < size <= MAX_FRAME:
        raise ReadError("resource", "辅助程序消息长度无效")
    try:
        value = json.loads(read_exact(stream, size).decode("utf-8"),
                           parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (ValueError, UnicodeError):
        raise ReadError("protocol", "辅助程序消息无效") from None
    if not isinstance(value, dict):
        raise ReadError("protocol", "辅助程序消息必须为对象")
    return value


def write_frame(stream, value):
    data = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8")
    if not 0 < len(data) <= MAX_FRAME:
        raise ReadError("resource", "辅助程序消息超过上限")
    stream.write(struct.pack(">I", len(data)))
    stream.write(data)
    stream.flush()


def envelope(kind, request_id, **fields):
    return {"protocol": PROTOCOL, "requestId": request_id, "kind": kind, **fields}


class Response:
    def __init__(self, status, headers, content):
        self.status_code, self.headers, self.content = status, headers, content

    @property
    def text(self):
        return self.content.decode("utf-8")

    def json(self):
        return json.loads(self.text)


class BridgeTransport:
    """No sockets, retries, redirects, cookies, cache or local credentials.

    Go must independently enforce destination policy, time and shared quotas.
    These Python limits are defense in depth, not a replacement for Go policy.
    """
    def __init__(self, reader, writer, request_id):
        self.reader, self.writer, self.request_id = reader, writer, request_id
        self.sequence = 0
        self.response_bytes = 0

    def get(self, url, *, headers=None):
        self.sequence += 1
        if self.sequence > MAX_SENDS:
            raise ReadError("resource", "请求次数超过上限")
        write_frame(self.writer, envelope("http.request", self.request_id,
                    sequence=self.sequence, method="GET", url=url, headers=dict(headers or {})))
        message = read_frame(self.reader)
        if (type(message.get("protocol")) is not int or message["protocol"] != PROTOCOL
                or message.get("requestId") != self.request_id
                or type(message.get("sequence")) is not int or message["sequence"] != self.sequence
                or message.get("kind") != "http.response"):
            raise ReadError("protocol", "响应标识或版本不匹配")
        if message.get("error"):
            code = message["error"]
            if code not in {"access", "restricted", "incomplete", "temporary", "cancelled", "resource"}:
                code = "protocol"
            raise ReadError(code, "受控传输未能完成请求")
        status, headers = message.get("status"), message.get("headers")
        if type(status) is not int or not isinstance(headers, dict) or not all(
            isinstance(k, str) and isinstance(v, str) for k, v in headers.items()
        ):
            raise ReadError("protocol", "HTTP 响应结构无效")
        if status in (401, 403):
            raise ReadError("access", "上游要求鉴权或拒绝访问")
        if status == 429:
            raise ReadError("restricted", "上游限制请求")
        if not 200 <= status < 300:
            raise ReadError("temporary" if status >= 500 else "access", "上游请求失败")
        encoded = message.get("body")
        if not isinstance(encoded, str) or len(encoded) > ((MAX_RESPONSE + 2) // 3) * 4:
            raise ReadError("resource", "HTTP 响应超过上限")
        try:
            content = base64.b64decode(encoded, validate=True)
        except ValueError:
            raise ReadError("protocol", "HTTP 响应编码无效") from None
        self.response_bytes += len(content)
        if len(content) > MAX_RESPONSE or self.response_bytes > MAX_RESPONSE_TOTAL:
            raise ReadError("resource", "HTTP 响应超过上限")
        response = Response(status, headers, content)
        # GraphQL may return partial data together with errors under HTTP 200.
        if "api-partner.spotify.com" in url:
            try:
                body = response.json()
            except (ValueError, UnicodeError):
                raise ReadError("incomplete", "上游返回无效 JSON") from None
            if not isinstance(body, dict) or body.get("errors"):
                raise ReadError("incomplete", "上游返回未完成的查询")
        return response

    def close(self):
        pass
