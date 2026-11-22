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
	flag.StringVar(&cfg.ServerURL, "opencloud", "https://localhost:9200", "OpenCloud URL")
	nextcloud := flag.String("nextcloud", "", "use this Nextcloud server instead of OpenCloud, e.g. http://localhost:8082 (sign-in is by app password, no Pocket ID)")
	flag.StringVar(&cfg.ClientID, "client-id", "odraz-demo", "OIDC client ID registered for the gateway")
	flag.BoolVar(&cfg.Insecure, "insecure", false, "accept OpenCloud's self-signed certificate (demo only)")
	demoUser := flag.String("demo-user", "", "user for the one-click demo sign-in (default admin; alice with -nextcloud)")
	demoTokenFile := flag.String("demo-token-file", "", "file with the demo user's app token or password; if it exists, the login page offers \"Try the demo\" (default .demo/app-token, or .demo/nextcloud-token with -nextcloud). Demo only: anyone who can reach the gateway gets in")
	flag.Parse()
	cfg.Backend = "opencloud"
	if *nextcloud != "" {
		cfg.Backend, cfg.ServerURL = "nextcloud", *nextcloud
		if *demoUser == "" {
			*demoUser = "alice"
		}
		if *demoTokenFile == "" {
			*demoTokenFile = ".demo/nextcloud-token"
		}
	}
	if *demoUser == "" {
		*demoUser = "admin"
	}
	if *demoTokenFile == "" {
		*demoTokenFile = ".demo/app-token"
	}
	if b, err := os.ReadFile(*demoTokenFile); err == nil && strings.TrimSpace(string(b)) != "" {
		cfg.DemoUser, cfg.DemoToken = *demoUser, strings.TrimSpace(string(b))
		log.Printf("one-click demo sign-in enabled as %q", cfg.DemoUser)
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
	log.Printf("odraz demo gateway on %s (public URL %s, %s %s)", *listen, cfg.PublicURL, cfg.Backend, cfg.ServerURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
