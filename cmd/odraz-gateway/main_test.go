package main

import "testing"

func TestParseBackend(t *testing.T) {
	b, err := parseBackend("vendor=opencloud,url=https://cloud.example.com/,insecure=true")
	if err != nil || b.ID != "opencloud" || b.URL != "https://cloud.example.com" || !b.Insecure {
		t.Fatalf("%+v %v", b, err)
	}
	if b, err = parseBackend("id=nc, vendor=nextcloud, url=http://nc.test"); err != nil || b.ID != "nc" || b.Insecure {
		t.Fatalf("%+v %v", b, err)
	}
	for _, bad := range []string{"", "vendor=owncloud,url=https://x", "vendor=opencloud", "vendor=opencloud,url=ftp://x", "vendor=opencloud,url=https://x,insecure=maybe", "vendor=opencloud,url=https://x,color=red", "justtext"} {
		if _, err := parseBackend(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
