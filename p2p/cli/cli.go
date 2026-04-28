// Package cli provides the interactive terminal interface for the P2P node.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"p2p/node"
)

// CLI drives the interactive terminal session for a running Node.
type CLI struct {
	node   *node.Node
	scanner *bufio.Scanner
}

// New returns a CLI bound to n.
func New(n *node.Node) *CLI {
	return &CLI{
		node:    n,
		scanner: bufio.NewScanner(os.Stdin),
	}
}

// Run blocks on stdin, parsing commands until /quit or EOF.
func (c *CLI) Run() {
	c.node.OnChat(func(nick, text string, ts time.Time) {
		fmt.Printf("\r[%s] %s: %s\n> ", ts.Format("15:04"), nick, text)
	})
	c.node.OnFile(func(path string) {
		fmt.Printf("\r[file] received → %s\n> ", path)
	})
	c.node.OnPeer(func(info node.PeerInfo) {
		ext := ""
		if info.ExtAddr != "" {
			ext = "  ext=" + info.ExtAddr
		}
		fmt.Printf("\r[+] %s (%s)%s connected\n> ", info.Nick, info.Addr, ext)
	})
	c.node.OnPeerLeave(func(addr string) {
		fmt.Printf("\r[-] peer %s disconnected\n> ", addr)
	})

	fmt.Println("P2P client ready. Type /help for commands.")
	fmt.Print("> ")

	for c.scanner.Scan() {
		line := strings.TrimSpace(c.scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}
		if !c.dispatch(line) {
			return
		}
		fmt.Print("> ")
	}
}

// dispatch handles a single line of input.
// Returns false to signal that the CLI should exit.
func (c *CLI) dispatch(line string) bool {
	if !strings.HasPrefix(line, "/") {
		// Bare text → broadcast chat.
		c.node.SendChat(line)
		return true
	}

	parts := strings.Fields(line)
	cmd := parts[0]

	switch cmd {
	case "/connect":
		if len(parts) < 2 {
			fmt.Println("usage: /connect <host:port>")
			return true
		}
		// Bootstrap also requests the peer list so we join the wider swarm.
		if err := c.node.Bootstrap(parts[1]); err != nil {
			fmt.Printf("[error] %v\n", err)
		} else {
			fmt.Printf("[info] connecting to %s…\n", parts[1])
		}

	case "/peers":
		peers := c.node.Peers()
		if len(peers) == 0 {
			fmt.Println("no peers connected")
		} else {
			for _, p := range peers {
				crypto := ""
				if p.Crypto {
					crypto = " [crypto]"
				}
				ext := ""
				if p.ExtAddr != "" {
					ext = "  ext=" + p.ExtAddr
				}
				fmt.Printf("  %s  (%s)%s%s\n", p.Addr, p.Nick, crypto, ext)
			}
		}

	case "/info":
		fmt.Printf("  nick    : %s\n", c.node.Nick())
		fmt.Printf("  listen  : %s\n", c.node.ListenAddr())
		fmt.Printf("  crypto  : %v\n", c.node.CryptoEnabled())
		if ext := c.node.ExternalAddr(); ext != "" {
			fmt.Printf("  ext addr: %s  ← share this for internet connections\n", ext)
		} else {
			fmt.Println("  ext addr: (not available — STUN may have failed)")
		}

	case "/send":
		if len(parts) < 3 {
			fmt.Println("usage: /send <host:port> <filepath>")
			return true
		}
		addr, path := parts[1], parts[2]
		go func() {
			if err := c.node.SendFile(addr, path); err != nil {
				fmt.Printf("\r[error] send file: %v\n> ", err)
			}
		}()

	case "/nick":
		fmt.Println("[info] nick can only be set at startup via --nick")

	case "/help":
		fmt.Println(helpText)

	case "/quit":
		fmt.Println("bye.")
		c.node.Close()
		return false

	default:
		fmt.Printf("unknown command %q — type /help\n", cmd)
	}
	return true
}

const helpText = `commands:
  /connect <host:port>        dial a peer (also fetches peer list)
  /peers                      list connected peers
  /info                       show this node's address and ext addr
  /send <host:port> <path>    send a file to a peer
  /help                       show this message
  /quit                       exit
  <anything else>             broadcast as chat message`
