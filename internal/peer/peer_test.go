package peer

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestParseName(t *testing.T) {
	valid := []string{"laptop", "desktop-2", "a", "0", strings.Repeat("a", 63)}
	for _, s := range valid {
		if _, err := ParseName(s); err != nil {
			t.Errorf("ParseName(%q) = %v, want ok", s, err)
		}
	}
	invalid := []string{"", "Laptop", "lap top", "lap.top", "-laptop", "laptop-", "läptop", strings.Repeat("a", 64)}
	for _, s := range invalid {
		if _, err := ParseName(s); err == nil {
			t.Errorf("ParseName(%q) = ok, want error", s)
		}
	}
}

func TestNameFromHost(t *testing.T) {
	for _, host := range []string{"laptop", "laptop.home.arpa", "laptop.home.arpa."} {
		n, err := NameFromHost(host)
		if err != nil || n != "laptop" {
			t.Errorf("NameFromHost(%q) = %q, %v; want laptop", host, n, err)
		}
	}
	if _, err := NameFromHost("laptop.local"); err == nil {
		t.Error("NameFromHost(laptop.local) = ok, want error")
	}
}

func TestMessageRoundTrip(t *testing.T) {
	data, err := encode(message{Name: "laptop", Reply: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "laptop" || !m.Reply {
		t.Errorf("decode(encode) = %+v", m)
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, raw := range []string{
		`not json`,
		`{"v":2,"name":"laptop"}`,
		`{"v":1}`,
		`{"v":1,"name":"Bad Name"}`,
	} {
		if _, err := decode([]byte(raw)); err == nil {
			t.Errorf("decode(%s) = ok, want error", raw)
		}
	}
}

func TestBroadcastOf(t *testing.T) {
	cases := []struct {
		cidr string
		want string
	}{
		{"192.168.1.79/24", "192.168.1.255"},
		{"10.1.2.3/8", "10.255.255.255"},
		{"172.17.0.1/16", "172.17.255.255"},
		{"192.168.1.1/30", "192.168.1.3"},
		{"192.168.1.1/31", ""},
		{"192.168.1.1/32", ""},
		{"fe80::1/64", ""},
	}
	for _, c := range cases {
		ip, ipnet, err := net.ParseCIDR(c.cidr)
		if err != nil {
			t.Fatal(err)
		}
		ipnet.IP = ip
		got, ok := broadcastOf(ipnet)
		if c.want == "" {
			if ok {
				t.Errorf("broadcastOf(%s) = %s, want none", c.cidr, got)
			}
			continue
		}
		if !ok || got.String() != c.want {
			t.Errorf("broadcastOf(%s) = %s, %v; want %s", c.cidr, got, ok, c.want)
		}
	}
}

func TestLoadDropsExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	fresh := Entry{Name: "laptop", Addr: netip.MustParseAddr("192.168.1.166"), Seen: now.Add(-Expiry + time.Second)}
	stale := Entry{Name: "desktop", Addr: netip.MustParseAddr("192.168.1.79"), Seen: now.Add(-Expiry)}
	if err := save(path, []Entry{stale, fresh}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != fresh {
		t.Errorf("Load = %+v, want only %+v", got, fresh)
	}
	if _, err := Resolve(path, "desktop", now); err == nil {
		t.Error("Resolve(expired peer) = ok, want error")
	}
	if addr, err := Resolve(path, "laptop", now); err != nil || addr != fresh.Addr {
		t.Errorf("Resolve(laptop) = %s, %v; want %s", addr, err, fresh.Addr)
	}
}

func TestLoadMissingTable(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "peers.json"), time.Now())
	if err != nil || len(got) != 0 {
		t.Errorf("Load(missing) = %v, %v; want empty", got, err)
	}
}

type recorder struct {
	mu   sync.Mutex
	seen map[Name]netip.Addr
}

func (r *recorder) record(entries []Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = map[Name]netip.Addr{}
	for _, e := range entries {
		r.seen[e.Name] = e.Addr
	}
}

func (r *recorder) has(name Name) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.seen[name]
	return ok
}

func startNode(t *testing.T, name Name, rec *recorder) (*Node, netip.AddrPort) {
	t.Helper()
	n, err := Listen(name, 0, filepath.Join(t.TempDir(), "peers.json"), rec.record)
	if err != nil {
		t.Fatal(err)
	}
	n.targets = func() ([]netip.AddrPort, error) { return nil, nil }
	port := n.conn.LocalAddr().(*net.UDPAddr).Port
	return n, netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))
}

func TestAnnounceIsAnsweredByUnicast(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var laptopSeen, phoneSeen recorder
	laptop, laptopAddr := startNode(t, "laptop", &laptopSeen)
	phone, _ := startNode(t, "phone", &phoneSeen)
	phone.targets = func() ([]netip.AddrPort, error) { return []netip.AddrPort{laptopAddr}, nil }

	errs := make(chan error, 2)
	go func() { errs <- laptop.Run(ctx) }()
	go func() { errs <- phone.Run(ctx) }()

	deadline := time.After(5 * time.Second)
	for !laptopSeen.has("phone") || !phoneSeen.has("laptop") {
		select {
		case err := <-errs:
			t.Fatalf("node stopped early: %v", err)
		case <-deadline:
			t.Fatal("laptop and phone did not learn each other within 5s")
		case <-time.After(10 * time.Millisecond):
		}
	}

	addr, err := Resolve(phone.tablePath, "laptop", time.Now().UTC())
	if err != nil || addr != netip.MustParseAddr("127.0.0.1") {
		t.Errorf("phone Resolve(laptop) = %s, %v", addr, err)
	}

	cancel()
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("Run after cancel = %v, want nil", err)
		}
	}
}

func TestBroadcastTargetsWithoutInterfaceAccess(t *testing.T) {
	orig := listInterfaces
	t.Cleanup(func() { listInterfaces = orig })
	listInterfaces = func() ([]net.Interface, error) {
		return nil, &net.OpError{Op: "route", Net: "ip+net", Err: os.NewSyscallError("netlinkrib", syscall.EACCES)}
	}
	got, err := broadcastTargets(15354)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != netip.MustParseAddrPort("255.255.255.255:15354") {
		t.Errorf("broadcastTargets with interfaces hidden = %v, want the limited broadcast", got)
	}

	listInterfaces = func() ([]net.Interface, error) { return nil, os.NewSyscallError("netlinkrib", syscall.ENOBUFS) }
	if _, err := broadcastTargets(15354); err == nil {
		t.Error("broadcastTargets with an unrelated interface error = ok, want error")
	}
}
