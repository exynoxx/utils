package main

import (
	"flag"
	"fmt"
	"log"
	"net"
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
	announce := flag.String("announce", "", "public IP (or ip:port) to advertise as a dialable address (full-cone NAT)")
	flag.Parse()

	cfg := node.Config{
		ListenPort:    *port,
		Nick:          *nick,
		DownloadsDir:  *downloadsDir,
		Bootstrap:     splitList(*bootstrap),
		Relays:        splitList(*relay),
		SharedFolders: splitList(*share),
		LAN:           *lan,
		Announce:      strings.TrimSpace(*announce),
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

	// Print the single best dialable address so a peer can copy it as a
	// --bootstrap value. (A public address appears once discovered, or
	// immediately when --announce is set; otherwise this is a LAN address.)
	if best := n.BestShareAddr(); best != "" {
		fmt.Printf("\n  ► copy this to connect from another PC:\n    %s\n\n", best)
	}

	// Phone sharing: print a LAN URL the user can type into a phone browser.
	if *uiPort > 0 {
		if ip := lanIP(); ip != "" {
			fmt.Println("  share files with your phone — open this in the phone's browser:")
			fmt.Printf("    http://%s:%d/phone\n\n", ip, *uiPort)
		} else {
			fmt.Print("  (no LAN IP detected — phone sharing needs the PC on a local network)\n\n")
		}
	}

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

// lanIP returns the PC's primary LAN IPv4 address, suitable for a phone on the
// same network to connect to. It first asks the OS which source address would
// be used to reach a public host (no packets are sent — UDP "connect" only
// selects a route), then falls back to scanning interfaces for a private IPv4
// if that fails (e.g. no default route). Returns "" if none is found.
func lanIP() string {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if ip := addr.IP.To4(); ip != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if ip != nil && !ip.IsLoopback() && ip.IsPrivate() {
			return ip.String()
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-(n-1):]
}
