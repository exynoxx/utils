package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"p2p/cli"
	"p2p/discovery"
	"p2p/node"
	"p2p/web"
)

func main() {
	port         := flag.Int("port", 9000, "TCP port to listen on")
	nick         := flag.String("nick", defaultNick(), "display name")
	crypto       := flag.Bool("crypto", false, "enable NaCl end-to-end encryption")
	bootstrap    := flag.String("bootstrap", "", "bootstrap peer address (host:port)")
	uiPort       := flag.Int("ui", 8080, "HTTP port for the browser UI (0 = disabled)")
	stun         := flag.String("stun", "stun.l.google.com:19302", "STUN server for NAT traversal (empty = disabled)")
	downloadsDir := flag.String("downloads", defaultDownloadsDir(), "directory for received files")
	discoPort    := flag.Int("disco-port", 9009, "UDP port for LAN auto-discovery (0 = disabled)")
	share        := flag.String("share", "", "comma-separated shared folder names (e.g. docs,photos)")
	flag.Parse()

	// STUN discovery is now handled inside node.Start() so that the UDP
	// socket is bound to the same port as the TCP listener.  We pass the
	// server address through the config.

	sharedFolders := []string{}
	if *share != "" {
		for _, name := range strings.Split(*share, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				sharedFolders = append(sharedFolders, name)
			}
		}
	}

	cfg := node.Config{
		ListenAddr:    fmt.Sprintf(":%d", *port),
		Nick:          *nick,
		Crypto:        *crypto,
		DownloadsDir:  *downloadsDir,
		STUNServer:    *stun,
		SharedFolders: sharedFolders,
	}

	n, err := node.New(cfg)
	if err != nil {
		log.Fatalf("create node: %v", err)
	}

	if err := n.Start(); err != nil {
		log.Fatalf("start node: %v", err)
	}

	if *bootstrap != "" {
		if err := n.Bootstrap(*bootstrap); err != nil {
			log.Fatalf("bootstrap %s: %v", *bootstrap, err)
		}
	}

	if *discoPort > 0 {
		svc := discovery.New(*nick, *port, *discoPort, func(addr string) {
			if err := n.Bootstrap(addr); err != nil {
				fmt.Printf("[info] discovery: could not connect to %s: %v\n", addr, err)
			}
		})
		if err := svc.Start(); err != nil {
			fmt.Printf("[warn] LAN discovery unavailable: %v\n", err)
		}
	}

	fmt.Printf("\n┌─────────────────────────────────────┐\n")
	fmt.Printf("│  p2p node started                   │\n")
	fmt.Printf("├─────────────────────────────────────┤\n")
	fmt.Printf("│  nick     : %-23s  │\n", *nick)
	fmt.Printf("│  listen   : :%-22d  │\n", *port)
	fmt.Printf("│  crypto   : %-23v  │\n", *crypto)
	fmt.Printf("│  downloads: %-23s  │\n", truncate(*downloadsDir, 23))
	if ext := n.ExternalAddr(); ext != "" {
		fmt.Printf("│  ext addr : %-23s  │\n", ext)
	}
	if *bootstrap != "" {
		fmt.Printf("│  bootstrap: %-23s  │\n", truncate(*bootstrap, 23))
	}
	if *discoPort > 0 {
		fmt.Printf("│  LAN disco: UDP %-20d  │\n", *discoPort)
	}
	if len(sharedFolders) > 0 {
		fmt.Printf("│  share    : %-23s  │\n", truncate(strings.Join(sharedFolders, ","), 23))
	}
	if *uiPort > 0 {
		fmt.Printf("│  UI       : http://localhost:%-8d  │\n", *uiPort)
	}
	fmt.Printf("└─────────────────────────────────────┘\n\n")

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-(n-1):]
}
