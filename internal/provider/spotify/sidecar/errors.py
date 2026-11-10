"""Dependency-free protocol error shared by helper and build-time verifier."""


class ReadError(Exception):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
