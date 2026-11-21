package daemon

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/peer"
)

func TestRefreshDNSZonesServesPeers(t *testing.T) {
	root := setupSrvRoot(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.cancel)

	upstream := startStubUpstream(t, "10.9.9.9")
	seedDNSInputs(t, root, upstream)
	bindTestDNSServer(t, d)

	peers := []peer.Entry{{Name: "laptop", Addr: netip.MustParseAddr("192.168.1.166"), Seen: time.Now().UTC()}}
	d.peers.Store(&peers)
	d.refreshDNSZones()

	addr := d.dns.Load().Addr()
	for name, wantIP := range map[string]string{
		"laptop.home.arpa":  "192.168.1.166",
		"LAPTOP.home.arpa":  "192.168.1.166",
		"desktop.home.arpa": "10.9.9.9",
		"exact.test":        "127.0.0.1",
	} {
		if got := queryA(t, addr, name); got != wantIP {
			t.Errorf("%s = %q, want %q", name, got, wantIP)
		}
	}
}
