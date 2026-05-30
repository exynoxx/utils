package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"p2p/cli"
	"p2p/node"
	"p2p/web"
)

func main() {
	port := flag.Int("port", 9000, "TCP+QUIC port to listen on (IPv4 and IPv6)")
	nick := flag.String("nick", defaultNick(), "display name")
	uiPort := flag.Int("ui", 8080, "HTTP port for the browser UI (0 = disabled)")
	downloadsDir := flag.String("downloads", "downloads", "directory for received files")
	lan := flag.Bool("lan", true, "enable mDNS LAN auto-discovery")
	openBrowser := flag.Bool("open", true, "open the UI in the default browser on startup")
	share := flag.String("share", "", "comma-separated shared folder names (e.g. docs,photos)")
	flag.Parse()

	cfg := node.Config{
		ListenPort:    *port,
		Nick:          *nick,
		DownloadsDir:  *downloadsDir,
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

	if *uiPort > 0 {
		// Phone sharing: print a LAN URL the user can type into a phone browser.
		if ip := lanIP(); ip != "" {
			fmt.Println("  share files with your phone — open this in the phone's browser:")
			fmt.Printf("    http://%s:%d/phone\n\n", ip, *uiPort)
		} else {
			fmt.Print("  (no LAN IP detected — phone sharing needs the PC on a local network)\n\n")
		}

		addr := fmt.Sprintf(":%d", *uiPort)
		srv := web.New(n, *downloadsDir)
		go func() {
			if err := srv.ListenAndServe(addr); err != nil {
				log.Printf("[error] web server: %v", err)
			}
		}()

		// Open the UI in the default browser once the server is accepting
		// connections, so the browser doesn't race the listener bind.
		if *openBrowser {
			go func() {
				probe := fmt.Sprintf("127.0.0.1:%d", *uiPort)
				for i := 0; i < 50; i++ {
					if c, err := net.DialTimeout("tcp", probe, 200*time.Millisecond); err == nil {
						c.Close()
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				web.OpenURL(fmt.Sprintf("http://localhost:%d", *uiPort))
			}()
		}
	}

	cli.New(n).Run()
}

func defaultNick() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "peer"
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
