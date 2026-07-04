# gows-plus (Native Calls Fork)

WhatsApp engine in Go for [WAHA](https://waha.devlike.pro/), extended with native WhatsApp voice calls.

This repository is a fork of [devlikeapro/gows-plus](https://github.com/devlikeapro/gows-plus), the [whatsmeow](https://github.com/tulir/whatsmeow)-based Go engine that powers the `GOWS` engine in WAHA. On top of the upstream engine, this fork adds a complete native WhatsApp audio calling stack, ported and adapted from [WaCalls](https://github.com/JotaDev66/WaCalls). The result is a self-contained Go binary that can place, receive, and stream audio on 1:1 WhatsApp voice calls, exposed to WAHA over gRPC and, through it, over the WAHA HTTP API and dashboard.

## Overview

The upstream engine implements sessions, messaging, media, groups, and related WhatsApp features over gRPC. This fork keeps all of that intact and adds a native calling layer:

- Native 1:1 voice calls (outgoing and incoming), implemented in pure Go, with no cgo required for the media stack.
- Meta's MLow voice codec, RTP/SRTP packetization, STUN, the SCTP relay transport, and the `<call>` signaling, integrated with whatsmeow.
- Two interchangeable media paths:
  - A WebRTC data channel that carries raw 16 kHz PCM between a browser and the engine (microphone uplink and peer downlink).
  - An optional file-based path that plays a PCM file as the outgoing audio and records the peer audio to a file (headless operation).
- New gRPC methods on the engine service: `StartCall`, `AcceptCall`, `EndCall`, `RejectCall`, and `WebRTC` (SDP exchange).

## Architecture

The calling stack lives under `src/voip`.

| Path | Responsibility |
| --- | --- |
| `voip/core` | Domain types, constants, and the `VoipSocket` interface |
| `voip/wanode` | Shared WhatsApp node and JID helpers |
| `voip/media` | MLow codec (vendored pure Go `mlow/`), RTP, SRTP, SSRC, PCM helpers, key derivation |
| `voip/transport` | SCTP relay, STUN, subscription encoding |
| `voip/signaling` | `<call>` stanza build and parse, call-key crypto, relay-ack parsing |
| `voip/call` | `CallManager`, which orchestrates a single call end to end |
| `voip/wa` | `VoipSocket` adapter over whatsmeow internals |
| `voip/callctl` | Per-session call controller, whatsmeow call-event routing, the pion WebRTC bridge, and the file audio source and sink |

### How a call flows (outgoing)

1. `StartCall` builds and sends the `<call>` offer through whatsmeow.
2. The browser opens a WebRTC data channel; the engine answers the SDP with pion and bridges 16 kHz PCM in both directions.
3. The peer accepts; the engine receives the relay endpoints and hop-by-hop keys.
4. STUN binding and allocation run on WhatsApp relays and the SRTP media path becomes active.
5. Uplink: browser microphone PCM to MLow encode to SRTP to relay. Downlink: relay to SRTP to MLow decode to browser.
6. `EndCall`, or an inbound terminate stanza, tears the call down.

Only audio is exposed. The underlying `CallManager` retains its video capability, but the controller never negotiates or emits video.

## gRPC API (calls)

| Method | Purpose |
| --- | --- |
| `StartCall` | Place an outgoing audio call (`to`, optional `audioIn`, `audioOut`) |
| `AcceptCall` | Accept an incoming audio call (`id`, optional `audioIn`, `audioOut`) |
| `EndCall` | End an active or ringing call (`id`) |
| `RejectCall` | Reject an incoming call (`from`, `id`) |
| `WebRTC` | Exchange the browser SDP (`id`, `sdpOffer`) and return the `sdpAnswer` |

The controller also emits a native `CallStateEvent` on every state transition, streamed to WAHA alongside the existing `call.received`, `call.accepted`, and `call.rejected` events.

## Requirements

- Go 1.25 or newer.
- The pinned whatsmeow fork (declared through the `replace` directive in `src/go.mod`).
- libvips is required only for the existing image and thumbnail features, not for calls.

## Build

```bash
make all              # build-proto, test, build the binary
# or
docker build -t gows .
```

Releases publish `gows-amd64`, `gows-arm64`, and `gows.proto` as assets (see `.github/workflows/build.yaml`). WAHA downloads the binary and proto by tag from this repository.

## Configuration (calls)

| Variable | Default | Meaning |
| --- | --- | --- |
| `WAHA_WEBRTC_UDP_PORT` | unset | Fixed UDP port used to funnel all browser ICE traffic through a single mux. Recommended behind NAT or a Docker bridge. |
| `WAHA_PUBLIC_IP` | unset | Public IP or host advertised as the ICE host candidate (1:1 NAT). |

Without these variables, pion falls back to ephemeral ports and interface IP candidates, which works for localhost and flat LANs. Browser microphone access requires a secure context: `localhost` is allowed, and remote hosts require HTTPS.

## Tests

```bash
cd src && go test ./...   # media stack: MLow codec, SRTP, STUN, RTP, relay-ack, signaling, state machine
```

## Contributors

This fork builds directly on the work of:

- devlikeapro — WAHA and the upstream gows / gows-plus engine. https://github.com/devlikeapro
- Rajeh Taher (purpshell) — meowcaller, the WhatsApp VoIP calling engine reference. https://github.com/purpshell/meowcaller
- JotaDev66 — WaCalls, the native Go VoIP stack ported into this fork. https://github.com/JotaDev66/WaCalls

## Acknowledgements

- whatsmeow — Go WhatsApp Web protocol library. https://github.com/tulir/whatsmeow
- pion/webrtc — pure-Go WebRTC stack (ICE, DTLS, SCTP). https://github.com/pion/webrtc

## License

This is a fork of devlikeapro/gows-plus. Upstream licensing and WAHA PRO terms apply to the base engine; the native calling additions in this fork follow the same terms.
