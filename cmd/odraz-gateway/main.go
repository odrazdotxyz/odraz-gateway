// Command odraz-gateway is the Odraz web gateway: it signs users in to their storage servers, keeps their tokens
// server side and exposes the files API (see docs/api.md) to a web UI. It has no way to sign in without a real
// credential: the demo's one-click login lives in cmd/demo-gateway.
//
//	odraz-gateway -public-url https://files.example.com \
//	    -backend 'id=cloud,vendor=opencloud,url=https://cloud.example.com' \
//	    -backend 'id=nc,vendor=nextcloud,url=https://nc.example.com'
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/odrazdotxyz/demo/pkg/gateway"
)

// backends collects repeated -backend flags.
type backends []gateway.Backend

func (b *backends) String() string { return fmt.Sprint(len(*b)) }
func (b *backends) Set(v string) error {
	be, err := parseBackend(v)
	if err != nil {
		return err
	}
	*b = append(*b, be)
	return nil
}

// parseBackend reads "id=cloud,vendor=opencloud,url=https://host,insecure=true". Only vendor and url are required;
// id defaults to the vendor. insecure accepts that one server's self-signed certificate.
func parseBackend(v string) (gateway.Backend, error) {
	var be gateway.Backend
	for _, kv := range strings.Split(v, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			return be, fmt.Errorf("backend %q: %q is not key=value", v, kv)
		}
		switch k {
		case "id":
			be.ID = val
		case "vendor":
			be.Vendor = val
		case "url":
			be.URL = strings.TrimRight(val, "/")
		case "insecure":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return be, fmt.Errorf("backend %q: insecure must be true or false", v)
			}
			be.Insecure = b
		default:
			return be, fmt.Errorf("backend %q: unknown key %q (use id, vendor, url, insecure)", v, k)
		}
	}
	if be.Vendor != "opencloud" && be.Vendor != "nextcloud" {
		return be, fmt.Errorf("backend %q: vendor must be opencloud or nextcloud", v)
	}
	if !strings.HasPrefix(be.URL, "http://") && !strings.HasPrefix(be.URL, "https://") {
		return be, fmt.Errorf("backend %q: url must start with http:// or https://", v)
	}
	if be.ID == "" {
		be.ID = be.Vendor
	}
	return be, nil
}

func env(k string) string { return os.Getenv("ODRAZ_" + k) }

func envBool(k string) bool { b, _ := strconv.ParseBool(env(k)); return b }

func main() {
	cfg := gateway.Config{}
	var bs backends
	flag.Var(&bs, "backend", "a storage server: id=NAME,vendor=opencloud|nextcloud,url=URL[,insecure=true]; repeat for several (env ODRAZ_BACKENDS, separated by semicolons)")
	flag.StringVar(&cfg.Listen, "listen", orDefault(env("LISTEN"), "127.0.0.1:8080"), "address to listen on; env ODRAZ_LISTEN")
	flag.StringVar(&cfg.PublicURL, "public-url", orDefault(env("PUBLIC_URL"), "http://localhost:8080"), "URL browsers use to reach the gateway (https in production: cookies are then Secure; also the OIDC callback base); env ODRAZ_PUBLIC_URL")
	flag.StringVar(&cfg.ClientID, "client-id", orDefault(env("CLIENT_ID"), "odraz-gateway"), "OIDC client ID registered for the gateway in the identity provider; env ODRAZ_CLIENT_ID")
	flag.StringVar(&cfg.WebDir, "web-dir", env("WEB_DIR"), "serve the web UI from this folder (for example the UI's dist/) instead of the one built in; env ODRAZ_WEB_DIR")
	flag.BoolVar(&cfg.TrustProxy, "trust-proxy", envBool("TRUST_PROXY"), "behind a reverse proxy: use X-Forwarded-For for the client address; env ODRAZ_TRUST_PROXY")
	flag.Int64Var(&cfg.Limits.MaxUploadBytes, "max-upload-bytes", 0, "largest upload in bytes (default 5 GiB)")
	flag.IntVar(&cfg.Limits.AuthPerMinute, "auth-rate", 0, "sign-in attempts per minute per client address (default 20, -1 off)")
	flag.IntVar(&cfg.Limits.APIPerMinute, "api-rate", 0, "API calls per minute per client address (default 6000, -1 off)")
	sessionFile := flag.String("session-file", env("SESSION_FILE"), "keep sessions and tokens in this encrypted file so they survive a restart (needs ODRAZ_SESSION_KEY); env ODRAZ_SESSION_FILE")
	flag.StringVar(&cfg.Office.URL, "office-url", env("OFFICE_URL"), "document editor server for everyone (env ODRAZ_OFFICE_URL); empty: users may set their own")
	flag.StringVar(&cfg.Office.Secret, "office-secret", env("OFFICE_SECRET"), "the editor's JWT secret; prefer the env ODRAZ_OFFICE_SECRET (a flag shows in the process list)")
	flag.BoolVar(&cfg.Office.AllowPrivate, "office-allow-private", envBool("OFFICE_ALLOW_PRIVATE"), "let users' own editor URLs point at private addresses (Docker); env ODRAZ_OFFICE_ALLOW_PRIVATE")
	flag.StringVar(&cfg.Office.GatewayURL, "office-gateway-url", env("OFFICE_GATEWAY_URL"), "where the editor reaches this gateway if not the public URL; env ODRAZ_OFFICE_GATEWAY_URL")
	flag.StringVar(&cfg.Office.Listen, "office-listen", env("OFFICE_LISTEN"), "serve only the editor's callback routes on this extra address; env ODRAZ_OFFICE_LISTEN")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg.Logger = log
	cfg.Backends = bs
	if len(cfg.Backends) == 0 && env("BACKENDS") != "" {
		for _, v := range strings.Split(env("BACKENDS"), ";") {
			be, err := parseBackend(v)
			if err != nil {
				fatal(log, err)
			}
			cfg.Backends = append(cfg.Backends, be)
		}
	}
	if len(cfg.Backends) == 0 {
		fatal(log, fmt.Errorf("no backend: give at least one -backend (or ODRAZ_BACKENDS)"))
	}
	seen := map[string]bool{}
	for _, b := range cfg.Backends {
		if seen[b.ID] {
			fatal(log, fmt.Errorf("two backends have the id %q", b.ID))
		}
		seen[b.ID] = true
		log.Info("backend", "id", b.ID, "vendor", b.Vendor, "url", b.URL, "insecure", b.Insecure)
	}
	if cfg.Office.URL != "" && cfg.Office.Secret == "" {
		fatal(log, fmt.Errorf("-office-url needs the editor's secret: set ODRAZ_OFFICE_SECRET"))
	}

	switch {
	case *sessionFile != "":
		k, err := gateway.ParseKey(env("SESSION_KEY"))
		if err != nil {
			fatal(log, fmt.Errorf("-session-file needs ODRAZ_SESSION_KEY: %w", err))
		}
		if cfg.Store, err = gateway.NewFileStore(*sessionFile, k); err != nil {
			fatal(log, err)
		}
		log.Info("sessions are kept in an encrypted file", "file", *sessionFile)
	default:
		log.Warn("sessions are kept in memory only: every restart signs everyone out and forgets their editor settings (set -session-file and ODRAZ_SESSION_KEY)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := gateway.New(cfg).Run(ctx); err != nil {
		fatal(log, err)
	}
}

func orDefault(v, d string) string {
	if v != "" {
		return v
	}
	return d
}

func fatal(log *slog.Logger, err error) {
	log.Error("stopping", "error", err.Error())
	os.Exit(1)
}
