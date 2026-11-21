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
	"time"

	"github.com/odrazdotxyz/demo/internal/gateway"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	cfg := gateway.Config{}
	flag.StringVar(&cfg.PublicURL, "public-url", "http://localhost:8080", "URL browsers use to reach the gateway (must match the OIDC client's callback)")
	flag.StringVar(&cfg.OpenCloudURL, "opencloud", "https://localhost:9200", "OpenCloud URL")
	flag.StringVar(&cfg.ClientID, "client-id", "odraz-demo", "OIDC client ID registered for the gateway")
	flag.BoolVar(&cfg.Insecure, "insecure", false, "accept OpenCloud's self-signed certificate (demo only)")
	flag.Parse()

	srv := &http.Server{Addr: *listen, Handler: gateway.New(cfg), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	log.Printf("odraz demo gateway on %s (public URL %s, OpenCloud %s)", *listen, cfg.PublicURL, cfg.OpenCloudURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
