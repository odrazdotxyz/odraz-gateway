// Command gateway serves the demo web UI and the JSON API behind it.
//
//	go run ./cmd/gateway -insecure          # OpenCloud's demo certificate is self-signed
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/odrazdotxyz/demo/internal/gateway"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	cfg := gateway.Config{}
	flag.StringVar(&cfg.PublicURL, "public-url", "http://localhost:8080", "URL browsers use to reach the gateway (must match the OIDC client's callback)")
	flag.StringVar(&cfg.ClientID, "client-id", "odraz-demo", "OIDC client ID registered for the gateway (OpenCloud)")
	flag.BoolVar(&cfg.Insecure, "insecure", false, "accept self-signed certificates, e.g. OpenCloud's demo one (demo only)")
	flag.StringVar(&cfg.WebDir, "web-dir", "", "serve the page from this folder instead of the copy inside the binary: edit the page, refresh the browser (development)")
	oc := flag.String("opencloud", "https://localhost:9200", "OpenCloud URL (empty: do not use OpenCloud)")
	nc := flag.String("nextcloud", "", "Nextcloud URL, e.g. http://localhost:8082 (empty: do not use Nextcloud)")
	ocUser := flag.String("opencloud-demo-user", "admin", "OpenCloud user for the one-click demo sign-in")
	ocTok := flag.String("opencloud-token-file", ".demo/app-token", "file with that user's app token; if it exists, \"Try the demo\" connects OpenCloud with it")
	ncUser := flag.String("nextcloud-demo-user", "alice", "Nextcloud user for the one-click demo sign-in")
	ncTok := flag.String("nextcloud-token-file", ".demo/nextcloud-token", "file with that user's password; if it exists, \"Try the demo\" connects Nextcloud with it")
	flag.Parse()

	// The demo token files let anyone who can reach the gateway in. Demo only.
	secret := func(file string) string {
		b, err := os.ReadFile(file)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if *oc != "" {
		cfg.Backends = append(cfg.Backends, gateway.Backend{ID: "opencloud", Vendor: "opencloud", URL: *oc, DemoUser: *ocUser, DemoToken: secret(*ocTok)})
	}
	if *nc != "" {
		cfg.Backends = append(cfg.Backends, gateway.Backend{ID: "nextcloud", Vendor: "nextcloud", URL: *nc, DemoUser: *ncUser, DemoToken: secret(*ncTok)})
	}
	if len(cfg.Backends) == 0 {
		log.Fatal("no backend: give -opencloud and/or -nextcloud")
	}
	if cfg.WebDir != "" {
		log.Printf("serving the page from %s (edits show on refresh; Go changes still need a restart)", cfg.WebDir)
	}
	for _, b := range cfg.Backends {
		log.Printf("backend %s at %s (one-click demo sign-in: %v)", b.ID, b.URL, b.DemoToken != "")
	}

	srv := &http.Server{Addr: *listen, Handler: gateway.New(cfg), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	log.Printf("odraz demo gateway on %s (public URL %s)", *listen, cfg.PublicURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
