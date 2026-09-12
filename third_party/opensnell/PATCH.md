# OpenSnell nested-module patch

- Upstream: `github.com/missuo/opensnell` **v1.0.4**
- Original revision: `b682576a0b05c35f125d443478be82c0c24f6ce7`
- License: GPL-3.0-or-later (upstream LICENSE.md retained)

## Why this fork exists

Stock `DialTCP` honors `context.Context` only for the TCP connect. The CONNECT
header write (and HTTP/TLS obfs first record) has no deadline and is not closed
when the dial context is cancelled. Wrapping `DialTCP` in a goroutine and
abandoning it on timeout leaves blocked writes and sockets alive.

## Patch

`components/snell/client.go`:

1. Bound establishment and the initial CONNECT/obfs write with a socket deadline
   taken from the context deadline or `DialTimeout`.
2. Close the connection when the establishment context is cancelled.
3. Close every failed establishment path.
4. After a successful header write, stop the cancel watcher, reject a racing
   cancellation, and clear the establishment-only deadline.
5. The returned connection is not attached to the dial context.

Cryptography and protocol framing are unchanged.
