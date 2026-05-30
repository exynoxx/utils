package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"p2p/cli"
	"p2p/node"
	"p2p/web"
)

func main() {
	port := flag.Int("port", 9000, "TCP+QUIC port to listen on (IPv4 and IPv6)")
	nick := flag.String("nick", defaultNick(), "display name")
	bootstrap := flag.String("bootstrap", "", "comma-separated peer multiaddrs to dial on startup (/ip4/.../p2p/<id>)")
	uiPort := flag.Int("ui", 8080, "HTTP port for the browser UI (0 = disabled)")
	downloadsDir := flag.String("downloads", defaultDownloadsDir(), "directory for received files")
	lan := flag.Bool("lan", true, "enable mDNS LAN auto-discovery")
	relay := flag.String("relay", "", "comma-separated static relay multiaddrs (optional)")
	share := flag.String("share", "", "comma-separated shared folder names (e.g. docs,photos)")
	flag.Parse()

	cfg := node.Config{
		ListenPort:    *port,
		Nick:          *nick,
		DownloadsDir:  *downloadsDir,
		Bootstrap:     splitList(*bootstrap),
		Relays:        splitList(*relay),
		SharedFolders: splitList(*share),
		LAN:           *lan,
	}

	n, err := node.New(cfg)
	if err != nil {
		log.Fatalf("create node: %v", err)
	}

	if err := n.Start(); err != nil {
		log.Fatalf("start node: %v", err)
	}

	fmt.Printf("\n┌─────────────────────────────────────┐\n")
	fmt.Printf("│  p2p node started                   │\n")
	fmt.Printf("├─────────────────────────────────────┤\n")
	fmt.Printf("│  nick     : %-23s  │\n", truncate(*nick, 23))
	fmt.Printf("│  peer id  : %-23s  │\n", truncate(n.ID(), 23))
	fmt.Printf("│  port     : %-23d  │\n", *port)
	fmt.Printf("│  downloads: %-23s  │\n", truncate(*downloadsDir, 23))
	fmt.Printf("│  LAN disco: %-23v  │\n", *lan)
	if len(cfg.SharedFolders) > 0 {
		fmt.Printf("│  share    : %-23s  │\n", truncate(strings.Join(cfg.SharedFolders, ","), 23))
	}
	if *uiPort > 0 {
		fmt.Printf("│  UI       : http://localhost:%-8d  │\n", *uiPort)
	}
	fmt.Printf("└─────────────────────────────────────┘\n")

	// Print fully-qualified dialable addresses so a peer can copy one as a
	// --bootstrap value. (Public/relay addresses appear once discovered.)
	fmt.Println("\n  share one of these as a bootstrap address:")
	for _, a := range n.P2pAddrs() {
		fmt.Printf("    %s\n", a)
	}
	fmt.Println()

	if *uiPort > 0 {
		addr := fmt.Sprintf(":%d", *uiPort)
		srv := web.New(n, *downloadsDir)
		go func() {
			if err := srv.ListenAndServe(addr); err != nil {
				log.Printf("[error] web server: %v", err)
			}
		}()
	}

	cli.New(n).Run()
}

func defaultNick() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "peer"
}

// defaultDownloadsDir returns a "downloads" folder relative to the current
// working directory, so it is always predictable regardless of how the binary
// was invoked.
func defaultDownloadsDir() string {
	return "downloads"
}

// splitList splits a comma-separated flag value into trimmed, non-empty items.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-(n-1):]
}
