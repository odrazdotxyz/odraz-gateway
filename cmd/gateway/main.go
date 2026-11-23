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
	env := func(k string) string { return os.Getenv("ODRAZ_" + k) }
	flag.StringVar(&cfg.Office.URL, "office-url", env("OFFICE_URL"), "document editor server (ONLYOFFICE Document Server) for everyone; env ODRAZ_OFFICE_URL. Empty: each user may set their own in Settings")
	flag.StringVar(&cfg.Office.Secret, "office-secret", env("OFFICE_SECRET"), "the editor server's JWT secret; prefer the env ODRAZ_OFFICE_SECRET (a flag shows in the process list)")
	flag.BoolVar(&cfg.Office.AllowPrivate, "office-allow-private", env("OFFICE_ALLOW_PRIVATE") == "1" || env("OFFICE_ALLOW_PRIVATE") == "true", "let users' own editor URLs point at private/loopback addresses (Docker networks); env ODRAZ_OFFICE_ALLOW_PRIVATE")
	flag.StringVar(&cfg.Office.GatewayURL, "office-gateway-url", env("OFFICE_GATEWAY_URL"), "where the EDITOR reaches this gateway if not the public URL, e.g. http://host.docker.internal:8081; env ODRAZ_OFFICE_GATEWAY_URL")
	flag.StringVar(&cfg.Office.Listen, "office-listen", env("OFFICE_LISTEN"), "serve only the editor's callback routes on this extra address (e.g. 0.0.0.0:8081); env ODRAZ_OFFICE_LISTEN")
	flag.Parse()
	if cfg.Office.URL != "" && cfg.Office.Secret == "" {
		log.Fatal("-office-url needs the editor's secret: set ODRAZ_OFFICE_SECRET")
	}

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

	gw := gateway.New(cfg)
	srv := &http.Server{Addr: *listen, Handler: gw, ReadHeaderTimeout: 10 * time.Second}
	var officeSrv *http.Server
	if cfg.Office.URL != "" {
		log.Printf("document editor: %s (set by the environment; users cannot change it)", cfg.Office.URL)
	}
	if cfg.Office.Listen != "" {
		officeSrv = &http.Server{Addr: cfg.Office.Listen, Handler: gw.OfficeHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			log.Printf("editor callbacks only, on %s", cfg.Office.Listen)
			if err := officeSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		}()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
		if officeSrv != nil {
			_ = officeSrv.Shutdown(sh)
		}
	}()
	log.Printf("odraz demo gateway on %s (public URL %s)", *listen, cfg.PublicURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
