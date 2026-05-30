# Reduce p2p to LAN-only

## Context

The `p2p` app (libp2p-based chat / file-transfer / folder-sync) currently supports
**both** local LAN discovery (mDNS) and full public-internet connectivity: NAT
traversal (UPnP/NAT-PMP, AutoNAT, DCUtR hole-punching), Circuit Relay v2 +
AutoRelay, a manual `--announce` public-IP override, public-IP auto-detection,
and a manual "connect by pasting a peer multiaddr" flow. The UI prominently
displays the node's shareable dialable address, an announce/detect-IP control,
and a Connect box.

The goal is to **reduce scope to LAN only**. On a LAN, mDNS already
auto-discovers and connects to every peer on the segment, so all of the
internet-connectivity machinery and the address-sharing / connect UI are
removed. Identity labels (the memorable adjective-animal name derived from a
peer ID) stay — they are how peers are identified, not IP/connection info.

**Confirmed decision:** manual connect-by-address is removed entirely (rely
solely on mDNS auto-discovery); there is nothing to paste or share.

## What stays (LAN core)

mDNS discovery (`discovery/`), peer-list gossip for LAN mesh growth, chat, file
transfer, folder sync, the phone LAN UI, the graph UI, and always-encrypted
libp2p connections (plain TCP+QUIC listen still works fine on a LAN without the
NAT stack).

## Changes

### `node/node.go`
- **`Config`**: remove `Bootstrap`, `Relays`, `Announce` fields. Keep
  `ListenPort`, `Nick`, `DownloadsDir`, `SharedFolders`, `LAN`.
- **`Node` struct**: remove `relayMu`, `relayCands`, `announceMu`, `announce`.
- **`New`**: drop `announce`/`relayCands` initialisation and the static-relay
  seeding loop (current lines ~119-125).
- **`buildHost`**: keep only `libp2p.ListenAddrStrings(listen...)`. Remove
  `AddrsFactory`, `NATPortMap`, `EnableNATService`, `EnableHolePunching`,
  `EnableRelay`, `EnableRelayService`, `EnableAutoRelayWithPeerSource`. Update
  the doc comment to describe a plain LAN host.
- **Delete functions**: `addrsFactory`, `announceAddrs`, `getAnnounce`,
  `SetAnnounce`, `AnnounceAddr`, `relaySource`, `noteRelayCandidate`,
  `ExternalAddr`, `ShareAddrs`, `isIP4LinkLocal`, `BestShareAddr`,
  `firstPublicAddr`, `parseAddrInfo`, `Bootstrap`, `Connect`, `ListenAddr`.
- **`Start`**: remove the bootstrap-dial loop (current lines ~457-461). Keep
  mDNS startup and `gossipLoop`.
- **`setupPeer`**: remove the `firstPublicAddr` block that sets `p.ExtAddr`;
  drop `ExtAddr` from the `OnPeer` callback payload.
- **`handlePeerListRes`**: remove the `noteRelayCandidate(*ai)` call. Keep the
  rest (records addrs + dials not-yet-connected LAN peers). Gossip helpers
  `addrInfoToWire` / `wireToAddrInfo` stay.
- **`PeerInfo`** / **`Peers()`**: remove the `ExtAddr` field and its population.
- **Imports**: remove `net`, `sort`, `strconv`, and the `manet`
  (`go-multiaddr/net`) import (all now unused). `strings` and `peerstore` stay.
- Keep `P2pAddrs` (still used by CLI `/info`) and `CryptoEnabled`.

### `main.go`
- Remove the `--bootstrap`, `--relay`, `--announce` flags and the matching
  `node.Config` fields.
- Remove the "► copy this to connect from another PC" / `BestShareAddr()` block
  (current lines ~64-69). Keep the phone LAN-URL block and the startup info box
  (incl. `LAN disco`).

### `web/server.go`
- Remove routes and handlers: `POST /connect` (`handleConnect`),
  `POST /announce` (`handleAnnounce`), `GET /pubip` (`handlePubIP`).
- **`selfInfo`**: reduce to `Nick`, `Addr`, `Crypto`. Remove `ExtAddr`,
  `ShareAddr`, `ShareAddrs`, `Announce`.
- **`snapshot`**: set `Addr: s.node.ID()` (bare peer ID — no IP), drop the
  removed fields. `time` import stays (used elsewhere).

### `web/ui.html`
- Remove the entire `#connect-panel` block (the "Your address — share to
  connect" + Copy, the announce-IP input + Detect + Set, and the Connect box),
  plus its CSS (`#connect-panel`, `.cp-*`). Remove the `#top-ext` span.
- **JS**: trim `self_` to `{nick,addr,crypto}`. Delete `updateConnectPanel`,
  `copyMyAddr`, `copyText`, `setAnnounce`, `doConnect`, the `detect-ip` handler,
  all listeners bound to removed elements (`copy-addr`, `my-addr`, `set-ip`,
  `announce-ip`, `detect-ip`, `connect-btn`, `connect-addr`), and the now-dead
  `es.addEventListener('self', …)` handler.
- `updateTopBar` / `render`: replace `self_.share_addr||self_.addr` with
  `self_.addr`; drop the `top-ext` and `updateConnectPanel()` lines. The
  topbar/self-panel/graph keep showing the `shortName(self_.addr)` memorable
  label (identity, not an IP).
- Update `#empty-hint` copy from "Paste a peer's address in the Connect box →"
  to something like "Peers on your network appear automatically."

### `cli/cli.go`
- Remove the `/connect` case and its `helpText` line.
- Remove `ExtAddr` usage in the `OnPeer` callback and in `/peers`.
- In `/info`, remove the public-address block (current lines ~114-118); keep the
  local-addresses listing via `P2pAddrs()`.

### `README.md`
- Rewrite to LAN-only: update the intro and the libp2p blurb (drop "across the
  public internet through NAT firewalls"), delete the **Bootstrap**,
  **NAT traversal and internet connectivity**, and **Relay discovery** sections,
  reframe **Gossip** as LAN mesh growth (no relay discovery), drop the
  self-info "public (NAT) address" mention, remove `--bootstrap` / `--relay`
  from the flags table and `/connect` from the CLI table, and update the
  build/run examples to LAN-only invocations. Update the `node/` architecture
  line (remove "NAT stack").

## Notes
- `go-libp2p` is a single module dependency, so `go.mod` needs no edits; the
  unused relay/holepunch sub-packages simply stop being imported.
- Encryption is unchanged (libp2p Noise/TLS on every connection).

## Verification
1. `go build ./...` && `go vet ./...` && `go test ./...` — all pass (transfer
   and protocol tests are network-agnostic and should be unaffected).
2. Launch two nodes on the same machine (same LAN):
   `./p2p --port 9000 --nick alice --ui 8080`
   `./p2p --port 9001 --nick bob --ui 8081`
   Confirm they **auto-discover via mDNS** and connect with no manual step
   (each shows the other as a peer).
3. In `http://localhost:8080`: confirm there is **no** Connect box, no
   share-your-address display, and no announce/detect-IP control; confirm no IP
   addresses are shown anywhere; confirm the peer appears automatically.
4. Send a chat message and a file between the two nodes — both succeed.
5. Open `http://<lan-ip>:8080/phone` and confirm phone upload/download still
   works (LAN feature, untouched).
