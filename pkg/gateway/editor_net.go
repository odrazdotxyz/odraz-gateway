package gateway

import (
	"context"
	"net"
	"net/netip"

	"github.com/odrazdotxyz/odraz-gateway/internal/editor"
)

// checkHostPublic resolves host and refuses private and internal addresses, for a clear message when a user saves
// an editor URL. (The connection is checked again when made, which also covers a name that changes later.)
func checkHostPublic(ctx context.Context, host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		return checkAddr(ip)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil // cannot resolve now: the connection will fail by itself; nothing private is reached
	}
	for _, ip := range ips {
		if err := checkAddr(ip); err != nil {
			return err
		}
	}
	return nil
}

func checkAddr(ip netip.Addr) error {
	if editor.IsBlocked(ip) {
		return editor.ErrBlocked
	}
	return nil
}
