"""Private frozen helper. Input is framed data, never a command or script."""
import platform
import sys
from importlib.metadata import version

from adapter import ReadError, SCRAPER_VERSION, read_playlist
from protocol import PROTOCOL, BridgeTransport, envelope, read_frame, write_frame


def deny_network_and_execution(event, _args):
    # Defense in depth. Actual HTTP belongs to Go; no runtime downloads/tools.
    if event in {"socket.connect", "socket.connect_ex", "socket.getaddrinfo", "socket.bind",
                 "socket.sendto", "subprocess.Popen", "os.system", "os.posix_spawn", "os.exec"}:
        raise ReadError("access", "辅助程序禁止直接联网或启动外部程序")


def handshake(request_id):
    return envelope("hello", request_id, python=platform.python_version(),
                    spotifyScraper=version("SpotifyScraper"),
                    os=sys.platform, arch=platform.machine(), frozen=bool(getattr(sys, "frozen", False)))


def self_test():
    from tests.fixture_peer import FixturePeer
    peer = FixturePeer("large")
    snapshot = read_playlist(peer.ref, peer.transport)
    if len(snapshot["entries"]) != 205 or snapshot["completeness"]["pages"] != 3:
        raise ReadError("runtime_unavailable", "离线自检失败")
    write_frame(sys.stdout.buffer, {**handshake("self-test"), "selfTest": "ok", "rawSeen": 205})


def main():
    sys.addaudithook(deny_network_and_execution)
    request_id = ""
    try:
        if platform.python_version() != "3.12.13" or version("SpotifyScraper") != SCRAPER_VERSION:
            raise ReadError("runtime_unavailable", "辅助程序依赖版本不匹配")
        if sys.argv[1:] == ["--self-test"]:
            self_test()
            return 0
        if sys.argv[1:]:
            raise ReadError("input", "辅助程序参数无效")
        message = read_frame(sys.stdin.buffer)
        request_id = message.get("requestId")
        if (type(message.get("protocol")) is not int or message["protocol"] != PROTOCOL
                or message.get("kind") != "playlist.read" or not isinstance(request_id, str)
                or not 1 <= len(request_id) <= 128):
            request_id = ""
            raise ReadError("protocol", "请求版本或标识无效")
        write_frame(sys.stdout.buffer, handshake(request_id))
        transport = BridgeTransport(sys.stdin.buffer, sys.stdout.buffer, request_id)
        snapshot = read_playlist(message.get("playlistRef"), transport)
        write_frame(sys.stdout.buffer, envelope("result", request_id, snapshot=snapshot))
        return 0
    except ReadError as error:
        write_frame(sys.stdout.buffer, envelope("error", request_id, code=error.code, message=str(error)))
        return 1
    except Exception:
        write_frame(sys.stdout.buffer, envelope("error", request_id, code="runtime_unavailable",
                                               message="辅助程序执行失败"))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
