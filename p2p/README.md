# p2p

A fully decentralised peer-to-peer application for chat, file transfer, and folder sync across any network — including the public internet through NAT firewalls. No central server. No registration. No framework dependencies beyond the Go standard library and `golang.org/x/crypto`.

---

## Table of Contents

1. [How it works](#how-it-works)
2. [Auto-discovery](#auto-discovery)
3. [NAT traversal and internet connectivity](#nat-traversal-and-internet-connectivity)
4. [Encryption](#encryption)
5. [Chat](#chat)
6. [File transfer](#file-transfer)
7. [Shared folder sync](#shared-folder-sync)
8. [Browser UI](#browser-ui)
9. [Gossip and peer propagation](#gossip-and-peer-propagation)
10. [Wire protocol](#wire-protocol)
11. [Build and run](#build-and-run)
12. [All flags](#all-flags)
13. [CLI commands](#cli-commands)
14. [Architecture](#architecture)

---

## How it works

Each node listens on a single TCP port. When a peer connects, both sides exchange a plain-text `handshake` message that contains the peer's nick, their **listener** port, a crypto flag, and their external address (from STUN). From that point on all messages — chat, file chunks, peer lists, hole-punch signals — flow over the same persistent TCP connection using a simple 4-byte length-prefixed JSON framing.

A node can be the dialler or the acceptor; both roles are symmetric. Once two nodes are connected, they exchange peer lists and dial every address they don't already know, growing the mesh automatically.

```
Alice ──TCP──► Bob ──TCP──► Carol
  └──────────────TCP──────────┘
```

There is no central tracker or coordination server. Every node is equal.

---

## Auto-discovery

### LAN discovery (UDP broadcast)

On a local network, nodes find each other with zero configuration. Each node sends a UDP broadcast to `255.255.255.255` on the discovery port (default `9009`) every **5 seconds**. The payload is a small JSON announcement:

```json
{"nick": "alice", "listen_port": 9000}
```

Every node listens on the same discovery port. When an announcement arrives from an unknown sender, the receiving node dials `senderIP:listen_port` directly over TCP and bootstraps from it. Self-announcements (same IP and port as self) are silently ignored.

LAN discovery requires no flags — it is on by default. Set `--disco-port 0` to disable it.

### Bootstrap (manual, for internet)

To join a swarm across the internet, supply one known peer:

```
./p2p --port 9000 --nick alice --bootstrap 203.0.113.7:9001
```

After dialling the bootstrap peer, the node immediately sends a `peer_list_req` and dials all returned addresses. This transitively discovers the entire swarm in a single round trip.

---

## NAT traversal and internet connectivity

Most consumer internet connections sit behind NAT routers. Connecting two such peers directly — without a relay — requires opening a path through both routers simultaneously. This application implements the full standard technique: **STUN address discovery** followed by **UDP hole punching** with a **TCP simultaneous-open** over the punched path.

### Step 1 — STUN (external address discovery)

At startup, the node sends a standard RFC 5389 STUN Binding Request to a STUN server (default: `stun.l.google.com:19302`). The server returns the node's **external IP and port** as seen from the public internet. This external address is shared with all connected peers during handshake so that any peer learning the address can attempt to connect.

The UDP socket used for STUN is bound to the **same port number** as the TCP listener. This is the key that makes hole punching work: NAT routers create a port mapping per (protocol, internal-port) pair; by using the same port for both UDP probing and the eventual TCP connection, the same NAT mapping is reused.

### Step 2 — UDP hole punching

When a direct TCP connection fails (i.e. both peers are behind restrictive NAT), the following sequence runs automatically:

1. The initiating peer (`A`) broadcasts a `holepunch_req` message to all currently-connected peers. The message contains `A`'s external UDP address and the target peer `B`'s external UDP address, plus a random `token`.

2. Any already-connected peer relays the `holepunch_req` to `B` if they are connected to it. `B` responds with a `holepunch_ack` (also relayed back to `A`).

3. Both `A` and `B` start sending UDP probe datagrams to each other's external address every **150 ms**. Each datagram carries the `token` as confirmation. When a probe arrives from the remote address, both NAT routers have now created mappings that allow traffic to flow in both directions — the "hole" is punched.

4. With the hole open, `A` calls `DialTCP` using the **same local port** as the UDP socket (enforced with `SO_REUSEPORT` on Linux/macOS and `SO_REUSEADDR` on Windows). Because the NAT router already has an outbound mapping for that port, the TCP SYN is allowed through and `B`'s router forwards it.

The three connection attempts happen in order — direct TCP, then external TCP (for full-cone NAT), then hole punch — with a short timeout at each stage. If any attempt succeeds, the rest are abandoned.

---

## Encryption

Encryption is **optional** and **per-connection**. Mixed swarms work: an encrypted node can connect to a plain node and both will fall back to plain-text for that link while keeping other links encrypted.

Enable with `--crypto`:

```
./p2p --port 9000 --nick alice --crypto
```

### How it works

At startup, each `--crypto` node generates a **Curve25519** key pair using `golang.org/x/crypto/nacl/box`.

After the plain-text `handshake` (which only reveals nick, port, and the fact that both sides want crypto), both peers exchange their Curve25519 public keys in a `crypto_handshake` message. Each side then calls `box.Precompute` to derive a **shared secret** from their private key and the peer's public key. This shared key is never transmitted; it is computed independently on both sides using Diffie-Hellman.

Every subsequent message is encrypted with **XSalsa20-Poly1305** (NaCl `box.SealAfterPrecomputation`). Each message gets a fresh **24-byte random nonce** from `crypto/rand`, prepended to the ciphertext. The receiver extracts the nonce and calls `box.OpenAfterPrecomputation`; authentication failure returns an error and closes the connection.

Security properties:
- **Confidentiality**: XSalsa20 stream cipher, 256-bit key.
- **Integrity and authenticity**: Poly1305 MAC, verified on every message.
- **Forward secrecy**: key pairs are ephemeral (generated fresh at each startup, never stored to disk).
- **Replay protection**: nonces are randomly generated per message via `crypto/rand`.

---

## Chat

All connected peers are in the same chat room. Type any text at the CLI prompt (without a leading `/`) and it is broadcast to every peer simultaneously.

```
hello everyone
[14:32] alice: hello everyone
[14:32] bob: hey alice
```

Messages carry the sender's nick and a Unix timestamp.

In the browser UI, messages appear in a scrollable chat log with timestamps. The chat input is at the bottom of the sidebar.

---

## File transfer

### Sending a file (CLI)

```
/send 203.0.113.7:9001 /path/to/photo.jpg
```

### Sending a file (browser UI)

Drag a file onto a peer's node in the graph, or use the "Send File" panel to pick a peer and a file.

Dragging a file onto your own node in the center broadcasts it to **all** peers.

### How it works

1. The sender reads the file, computes a **SHA-256 checksum** of the entire content, then transmits a `file_meta` message with the file name, size, and checksum.
2. The file is split into **64 KB chunks**. Each chunk is sent as a `file_chunk` message with an index and a completion flag on the last chunk.
3. The receiver stores chunks in memory as they arrive. When the final chunk arrives, it reassembles them in order, writes the output to `downloads/`, and **verifies the SHA-256 checksum**. If the checksum does not match, the file is deleted and an error is logged.

Received files go to `./downloads/` (configurable with `--downloads`). Progress is printed to stdout during both send and receive.

---

## Shared folder sync

Shared folders are directories that are kept in sync across all peers who also share a folder with the same name.

### Starting with shared folders

```
./p2p --port 9000 --nick alice --share docs,photos
```

This watches `./docs/` and `./photos/` relative to the working directory.

### How it works

**Announcement**: when a new peer connects, each side sends a `folder_announce` message listing all of its shared folder names. If the remote peer shares a folder with the same name, both sides register each other as subscribers and perform an **initial full sync** — walking the entire folder tree and sending every file.

**Change detection**: a polling watcher checks each shared directory every **2 seconds**. It maintains a snapshot of every file's size and modification time. When a file is added or modified, it sends the updated file to all subscribed peers. When a file is deleted, it sends a `folder_delete` message.

**Receiving changes**: received files are written to the local copy of the shared folder. The watcher's snapshot is updated (`Refresh`) after writing to avoid re-broadcasting the file just received. Similarly, `RefreshDelete` prevents echoing a deletion back to the peer that triggered it.

**Last-write-wins**: if a received file has a modification time older than the local version, it is silently skipped.

**Path traversal protection**: all received relative paths are sanitised with `filepath.Clean`, rejected if absolute, and rejected if they start with `..`. This prevents a malicious peer from writing files outside the shared folder.

---

## Browser UI

Start the HTTP server with `--ui`:

```
./p2p --port 9000 --nick alice --ui 8080
```

Open `http://localhost:8080` in a browser.

### Features

- **Network graph**: an animated SVG showing your node in the centre connected to all peers. Encrypted connections are shown in blue with a lock label.
- **Live updates**: all events (peer connections, chat, file arrivals, folder changes) are delivered in real time via **Server-Sent Events (SSE)**.
- **Chat**: full chat log with timestamps; send from the input at the bottom.
- **File transfer**: drag a file onto a peer node, or use the file picker. Drag onto your own node to broadcast to all peers.
- **Received files**: list of all downloaded files, newest first, click to open with the OS default application.
- **Shared folders**: per-folder file list updated live.
- **Notifications**: toast popups for file arrivals and peer events, auto-dismissed after 4 seconds.
- **Self info panel**: shows your nick, listen address, external (NAT) address, and crypto status.

The UI is a single self-contained HTML file compiled into the binary with `//go:embed`.

---

## Gossip and peer propagation

Every **60 seconds**, each node broadcasts a `peer_list_req` to all connected peers. Each peer responds with the addresses of all peers it knows (excluding the requester). The requester then dials any unknown addresses.

This means the mesh self-heals over time: if two parts of the network become connected only through a third node, they will eventually discover each other directly and add a redundant path.

Peer list responses also include the external (STUN) address of each peer, enabling hole-punch attempts to peers behind NAT that are not yet directly reachable.

---

## Wire protocol

All messages use a simple binary framing over TCP:

```
[4 bytes: uint32 big-endian body length][N bytes: JSON body]
```

The JSON body is always:

```json
{"type": "message_type", "payload": { ... }}
```

Maximum message size is **128 MB**, enforced before allocation to guard against malformed length headers.

When encryption is enabled, the entire JSON body is replaced with:

```
[4 bytes: uint32 sealed length][24 bytes: nonce][ciphertext + 16-byte Poly1305 tag]
```

### Message types

| Type | Direction | Purpose |
|---|---|---|
| `handshake` | both, first | Nick, listen port, crypto flag, external addr |
| `crypto_handshake` | both, second | Curve25519 public key |
| `chat` | broadcast | Text message with nick and timestamp |
| `file_meta` | unicast | File name, size, SHA-256 checksum, transfer ID |
| `file_chunk` | unicast | 64 KB chunk with index and final flag |
| `peer_list_req` | unicast | Request peer addresses from a peer |
| `peer_list_res` | unicast | List of peer addresses + their external addrs |
| `holepunch_req` | broadcast/relay | Initiate coordinated UDP hole punch |
| `holepunch_ack` | broadcast/relay | Acknowledge and begin punching back |
| `folder_announce` | unicast | List of shared folder names |
| `folder_file_meta` | unicast | Shared folder file metadata (with relative path) |
| `folder_delete` | unicast | Notify peer to delete a file from shared folder |

---

## Build and run

```sh
go build ./...
go vet ./...

# Node 1 — LAN, no extras
./p2p --port 9000 --nick alice

# Node 2 — connect to alice, enable encryption
./p2p --port 9001 --nick bob --bootstrap 127.0.0.1:9000 --crypto

# Node 3 — internet peer with STUN, web UI, shared folder
./p2p --port 9002 --nick carol --bootstrap alice.example.com:9000 \
      --crypto --ui 8080 --share docs,photos

# Two internet nodes with no bootstrap (mutual manual connect)
./p2p --port 9000 --nick alice --stun stun.l.google.com:19302
# alice shares her external address; bob does:
./p2p --port 9000 --nick bob --bootstrap alice-external-ip:9000 --stun stun.l.google.com:19302
```

Received files are saved to `./downloads/` by default.

---

## All flags

| Flag | Default | Description |
|---|---|---|
| `--port` | `9000` | TCP (and UDP) listen port |
| `--nick` | hostname | Display name shown to peers |
| `--crypto` | off | Enable NaCl end-to-end encryption |
| `--bootstrap` | — | `host:port` of a peer to connect to on startup |
| `--stun` | `stun.l.google.com:19302` | STUN server for NAT traversal; set to empty to disable |
| `--ui` | `8080` | HTTP port for browser UI; `0` to disable |
| `--downloads` | `downloads` | Directory to save received files |
| `--disco-port` | `9009` | UDP port for LAN auto-discovery; `0` to disable |
| `--share` | — | Comma-separated folder names to watch and sync |

---

## CLI commands

| Input | Action |
|---|---|
| `<any text>` | Broadcast chat message to all peers |
| `/connect <host:port>` | Dial a peer and handshake |
| `/peers` | List all connected peers (address + nick) |
| `/send <host:port> <path>` | Send a file to a specific peer |
| `/nick` | Show your current nick (set at startup only) |
| `/help` | Print command reference |
| `/quit` | Disconnect all peers and exit |

---

## Architecture

```
main.go         flag parsing, wires everything together
├── node/       TCP listener, peer lifecycle, message dispatch, gossip
│   ├── peer.go         per-peer state, read/write loops
│   ├── peerstore.go    thread-safe peer map
│   └── folder.go       shared folder subscription and change fan-out
├── protocol/   wire format (length-prefix + JSON), NaCl crypto wrapper
├── transfer/   chunked file send/receive, SHA-256 verify
├── discovery/  LAN UDP broadcast discovery
├── holepunch/  STUN client, UDP hole punch, SO_REUSEPORT TCP dial
├── share/      polling directory watcher
├── cli/        stdin command loop
└── web/        HTTP server, SSE hub, embedded browser UI
```

No global state. No `init()` functions. Errors propagate up the call stack; non-fatal background errors are logged with `[warn]`. Each peer connection runs two goroutines (`readLoop`, `writeLoop`) plus a disconnect-cleanup goroutine. The gossip loop, accept loop, and each folder watcher are additional long-lived goroutines, all exiting cleanly via a `done` channel or connection close.
