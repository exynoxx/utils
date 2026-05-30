# p2p

A decentralised peer-to-peer application for chat, file transfer, and folder
sync across a **local network**. No central server. No registration. Peers on
the same LAN find each other automatically.

The networking core is built on [**go-libp2p**](https://github.com/libp2p/go-libp2p),
the reference peer-to-peer stack: multi-transport dialing (TCP + QUIC, IPv4 +
IPv6) and authenticated, encrypted connections. The application layer — chat,
file transfer, folder sync, browser UI, CLI — is original code layered on top.

---

## Table of Contents

1. [How it works](#how-it-works)
2. [Auto-discovery](#auto-discovery)
3. [Encryption and identity](#encryption-and-identity)
4. [Chat](#chat)
5. [File transfer](#file-transfer)
6. [Shared folder sync](#shared-folder-sync)
7. [Browser UI](#browser-ui)
8. [Phone sharing](#phone-sharing)
9. [Gossip and peer propagation](#gossip-and-peer-propagation)
10. [Wire protocol](#wire-protocol)
11. [Build and run](#build-and-run)
12. [All flags](#all-flags)
13. [CLI commands](#cli-commands)
14. [Architecture](#architecture)

---

## How it works

Each node runs a single **libp2p host**. A host has a cryptographic **peer ID**
(derived from its key pair) and listens on TCP and QUIC over both IPv4 and IPv6.
Peers are addressed by **multiaddr** — a self-describing address such as:

```
/ip4/192.168.1.20/tcp/9000/p2p/12D3KooW…
/ip4/192.168.1.20/udp/9000/quic-v1/p2p/12D3KooW…
```

The trailing `/p2p/<peer-id>` component means a multiaddr is self-authenticating:
libp2p verifies that the peer you reach actually owns that ID before the
connection is established.

When two nodes connect, libp2p negotiates a transport, performs an authenticated
encrypted handshake, and identifies the peer. The application then opens exactly
**one bidirectional stream** per peer over the app protocol `/p2p-app/1.0.0`. To
avoid both sides opening a stream at once, the peer with the
lexicographically-smaller peer ID opens it and the other accepts (a `peers.Has`
guard + `stream.Reset()` is the backstop). The first framed message on the
stream is an app-level `handshake` carrying just the peer's nick — everything
else (auth, encryption, identity) is already done by libp2p.

From there, all messages — chat, file chunks, peer lists, folder sync — flow
over that one stream using a simple length-prefixed framing (see
[Wire protocol](#wire-protocol)).

There is no central tracker or coordination server. Every node is equal.

---

## Auto-discovery

On a local network, nodes find each other with **zero configuration** via
libp2p's **mDNS** service. Each node advertises itself and watches for others on
the local segment; discovered peers are dialed automatically. There is nothing
to paste, share, or configure — start two nodes on the same LAN and they
connect. LAN discovery is on by default; disable it with `--lan=false`.

---

## Encryption and identity

Connections are **always** encrypted and peer-authenticated — there is no
plaintext mode and no `--crypto` flag. libp2p negotiates a **Noise** or
**TLS 1.3** handshake on every connection, and the peer ID cryptographically
binds the connection to a specific key pair, so a man-in-the-middle cannot
impersonate a peer.

**Identity is ephemeral.** A fresh key pair — and therefore a fresh peer ID — is
generated at each startup; nothing is persisted to disk. This gives forward
secrecy across runs. In the UI, peers are shown by their nick (or a stable,
memorable adjective-animal label derived from the peer ID) rather than a raw
address.

---

## Chat

All connected peers are in the same chat room. Type any text at the CLI prompt
(without a leading `/`) and it is broadcast to every peer simultaneously.

```
hello everyone
[14:32] alice: hello everyone
[14:32] bob: hey alice
```

Messages carry the sender's nick and a Unix timestamp. In the browser UI they
appear in a scrollable chat log; the input is at the bottom of the sidebar.

---

## File transfer

### Sending a file (CLI)

```
/send 12D3KooW…  /path/to/photo.jpg
```

The first argument is the recipient's **peer ID**.

### Sending a file (browser UI)

Drag a file onto a peer's node in the graph, or use the "Send File" panel to
pick a peer and a file. Dragging a file onto your own node in the centre
broadcasts it to **all** peers.

### How it works

1. The sender transmits a `file_meta` message with the file name, size, and a
   transfer ID.
2. The file is streamed in **1 MB chunks**, each sent as a `file_chunk` message
   whose raw bytes ride in the message's binary trailer (no base64 inflation).
   The SHA-256 checksum is computed while streaming and sent as a
   `file_checksum` trailer after the final chunk.
3. The receiver reassembles chunks in order, writes the output to `downloads/`,
   and **verifies the SHA-256 checksum**. On mismatch the file is deleted and an
   error logged. The receiver then sends a `file_ack` back to the sender.

Received files go to `./downloads/` (configurable with `--downloads`). Progress
is printed during both send and receive.

---

## Shared folder sync

Shared folders are directories kept in sync across all peers who also share a
folder with the same name.

### Starting with shared folders

```
./p2p --port 9000 --nick alice --share docs,photos
```

This watches `./docs/` and `./photos/` relative to the working directory.

### How it works

**Announcement**: when a new peer connects, each side sends a `folder_announce`
listing its shared folder names. If the remote shares a folder with the same
name, both register each other as subscribers and perform an **initial full
sync**, sending every file.

**Change detection**: a polling watcher checks each shared directory every **2
seconds**, maintaining a snapshot of each file's size and modification time.
Added/modified files are sent to subscribers (`folder_file_meta` + chunks);
deletions send a `folder_delete`.

**Receiving changes**: received files are written to the local copy. The
snapshot is updated after writing (`Refresh` / `RefreshDelete`) so the
just-received change is not echoed back.

**Last-write-wins**: a received file older than the local version is silently
skipped.

**Path traversal protection**: received relative paths are sanitised with
`filepath.Clean`, rejected if absolute, and rejected if they escape the folder
with `..`.

---

## Browser UI

Start the HTTP server with `--ui`:

```
./p2p --port 9000 --nick alice --ui 8080
```

Open `http://localhost:8080` in a browser (it opens automatically unless
`--open=false`).

### Features

- **Network graph**: an animated SVG showing your node connected to all peers
  (identified by nick / memorable name) and any connected phones.
- **Live updates**: all events (peer connections, chat, file arrivals, folder
  changes) stream in real time via **Server-Sent Events (SSE)**.
- **Chat**: full chat log with timestamps.
- **File transfer**: drag a file onto a peer node, or use the file picker. Drag
  onto your own node to broadcast to all.
- **Received files**: list of downloads, newest first, click to open with the OS
  default app.
- **Shared folders**: per-folder file list updated live.
- **Notifications**: toast popups for file arrivals and peer events.
- **Self info panel**: shows your nick and identity label.

The UI is a single self-contained HTML file compiled into the binary with
`//go:embed`.

---

## Phone sharing

The node serves a mobile-friendly page at `http://<lan-ip>:8080/phone` (the LAN
URL is printed at startup). A phone on the same network can:

- **Send files to the PC** — uploaded straight into `./downloads/` over the LAN
  (no p2p hop).
- **Receive files from the PC** — drag a file onto the phone's node in the
  desktop graph; the phone is notified over SSE with a one-time download link.

Connected phones appear as nodes in the desktop graph; presence is tracked with
lightweight heartbeats.

---

## Gossip and peer propagation

Every **60 seconds** (and once per newly-connected peer), each node exchanges
`peer_list_req` / `peer_list_res` messages. The payload is a set of
`peer.AddrInfo` — peer ID plus known multiaddrs, including the node's own
addresses. The recipient dials any peers it doesn't already know.

This grows and self-heals the LAN mesh and lets late joiners catch up to peers
already in the swarm.

---

## Wire protocol

Each application message is framed over the libp2p stream as:

```
[4 bytes: uint32 big-endian json_len]
[4 bytes: uint32 big-endian bin_len]
[json_len bytes: JSON-encoded message]
[bin_len  bytes: raw binary trailer]
```

The JSON body is always `{"type": "...", "payload": { ... }}`. The optional
binary trailer carries large blobs (file chunks) raw, avoiding base64 overhead.
Either length may be zero. Each length is capped at **128 MB**, enforced before
allocation to guard against malformed headers.

The connection itself is encrypted and authenticated by libp2p (Noise / TLS), so
there is no application-level crypto framing.

### Message types

| Type | Direction | Purpose |
|---|---|---|
| `handshake` | both, first | App-level hello — exchanges nick (libp2p already did auth/encryption/identity) |
| `chat` | broadcast | Text message with nick and timestamp |
| `file_meta` | unicast | File name, size, transfer ID |
| `file_chunk` | unicast | 1 MB chunk (raw bytes in binary trailer) with index and final flag |
| `file_checksum` | unicast | SHA-256 trailer sent after the final chunk |
| `file_ack` | unicast | Receiver → sender: file OK / failed |
| `peer_list_req` | unicast | Request known peers |
| `peer_list_res` | unicast | List of `peer.AddrInfo` (IDs + multiaddrs) |
| `folder_announce` | unicast | List of shared folder names |
| `folder_file_meta` | unicast | Shared-folder file metadata (with relative path + mod time) |
| `folder_delete` | unicast | Notify peer to delete a file from a shared folder |

---

## Build and run

```sh
go build ./...
go vet ./...
go test ./...

# Node 1 — web UI, LAN auto-discovery
./p2p --port 9000 --nick alice --ui 8080

# Node 2 — different port on the same machine / LAN; discovers node 1 via mDNS
./p2p --port 9001 --nick bob --ui 8081

# Node with a shared folder
./p2p --port 9002 --nick carol --ui 8082 --share docs,photos
```

Peers on the same LAN connect automatically — there is nothing to copy or paste.
Received files are saved to `./downloads/` by default.

---

## All flags

| Flag | Default | Description |
|---|---|---|
| `--port` | `9000` | TCP + QUIC listen port (IPv4 and IPv6) |
| `--nick` | hostname | Display name shown to peers |
| `--ui` | `8080` | HTTP port for browser UI; `0` to disable |
| `--open` | `true` | Open the UI in the default browser on startup |
| `--downloads` | `downloads` | Directory to save received files |
| `--lan` | `true` | Enable mDNS LAN auto-discovery (`--lan=false` to disable) |
| `--share` | — | Comma-separated shared folder names to watch and sync |

---

## CLI commands

| Input | Action |
|---|---|
| `<any text>` | Broadcast chat message to all peers |
| `/peers` | List connected peers (peer ID + nick) |
| `/info` | Show this node's peer ID and addresses |
| `/send <peer-id> <path>` | Send a file to a specific peer |
| `/nick` | Nick is set at startup only (via `--nick`) |
| `/help` | Print command reference |
| `/quit` | Disconnect all peers and exit |

---

## Architecture

```
main.go         flag parsing, wires everything together
├── node/       libp2p host, peer lifecycle, stream dispatch, gossip
│   ├── peer.go         per-peer state (stream + peer ID), read/write loops
│   ├── peerstore.go    thread-safe peer map
│   └── folder.go       shared folder subscription and change fan-out
├── protocol/   wire format (length-prefix + JSON + binary trailer), message types
├── transfer/   chunked file send/receive, SHA-256 verify
├── discovery/  libp2p mDNS LAN discovery wrapper
├── share/      polling directory watcher
├── cli/        stdin command loop
└── web/        HTTP server, SSE hub, embedded browser + phone UI
```

Networking primitives (transports, encryption, peer auth) are provided by
go-libp2p; the packages above implement the application on top. Each peer
connection runs `readLoop` and `writeLoop` goroutines plus a disconnect-cleanup
goroutine; the gossip loop and each folder watcher are additional long-lived
goroutines that exit cleanly on close.
```
