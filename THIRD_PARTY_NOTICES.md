# Third-party notices

Tarish v1.1.0 embeds a nested copy of OpenSnell for Snell v4/v5 TCP.

## OpenSnell

- Project: https://github.com/missuo/opensnell
- Version: v1.0.4
- Revision: b682576a0b05c35f125d443478be82c0c24f6ce7
- License: GPL-3.0-or-later (see `third_party/opensnell/LICENSE.md` and `LICENSE`)
- Local tree: `third_party/opensnell`
- Patch: `third_party/opensnell/PATCH.md`

The nested copy is included through a Go module `replace`. Cryptography and
protocol framing are unchanged. The only maintained patch bounds CONNECT/obfs
first writes with socket deadlines, closes failed and cancelled establishment
paths, and returns connections that are not tied to a short-lived dial context.

Corresponding source for a binary release is this repository, including
`third_party/opensnell`.

## XMRig

Bundled miner binaries remain the existing donate-free XMRig 6.26.0 artifacts
under `bin/`. See upstream https://github.com/xmrig/xmrig.
