package traefik

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// scratchResolv points resolvConfPath and nixosMarkerPath into a temp dir.
func scratchResolv(t *testing.T) (resolvPath, nixosPath string) {
	t.Helper()
	dir := t.TempDir()
	prevResolv, prevNixos := resolvConfPath, nixosMarkerPath
	resolvConfPath = filepath.Join(dir, "resolv.conf")
	nixosMarkerPath = filepath.Join(dir, "NIXOS")
	t.Cleanup(func() { resolvConfPath, nixosMarkerPath = prevResolv, prevNixos })
	return resolvConfPath, nixosMarkerPath
}

// fsShell is a fake whose sudo ln/tee act on the real (scratch) filesystem,
// so a test observes what the commands would have done.
func fsShell(t *testing.T) *shelltest.Fake {
	t.Helper()
	fake := shelltest.New(nil)
	fake.Handler = func(method, _ string, args []string, stdin string) (shelltest.Response, bool) {
		switch {
		case method == "SudoWrite":
			return shelltest.Response{Err: os.WriteFile(args[1], []byte(stdin), 0o644)}, true
		case method == "SudoRun" && len(args) == 4 && args[0] == "ln" && args[1] == "-sfn":
			_ = os.Remove(args[3])
			return shelltest.Response{Err: os.Symlink(args[2], args[3])}, true
		}
		return shelltest.Response{}, false
	}
	swapShell(t, fake)
	return fake
}

func TestNameservers(t *testing.T) {
	in := "# nameserver 9.9.9.9\nsearch lan\nnameserver\t10.0.0.1 # router\n  nameserver 1.1.1.1\nnameservers 2.2.2.2\nnameserver\n"
	var got []string
	for ns := range nameservers(in) {
		got = append(got, ns)
	}
	if want := []string{"10.0.0.1", "1.1.1.1"}; !slices.Equal(got, want) {
		t.Errorf("nameservers = %v, want %v", got, want)
	}
	if first := firstNameserver(in); first != "10.0.0.1" {
		t.Errorf("firstNameserver = %q", first)
	}
	if first := firstNameserver("search lan\n"); first != "" {
		t.Errorf("firstNameserver(no nameserver) = %q, want empty", first)
	}
}

func TestNameserversAllocatesNothing(t *testing.T) {
	in := "search lan\nnameserver 10.0.0.1\nnameserver 1.1.1.1\n"
	if n := testing.AllocsPerRun(100, func() {
		for range nameservers(in) {
		}
	}); n != 0 {
		t.Errorf("walking nameservers allocated %v times", n)
	}
}

func TestResolvedBypass(t *testing.T) {
	stub := "nameserver 127.0.0.53\noptions edns0 trust-ad\n"
	upstream := "nameserver 192.168.1.1\n"
	for _, tc := range []struct {
		name     string
		link     string // symlink target; "" writes a regular file
		contents string
		nixos    bool
		want     *ResolvedBypassError
	}{
		{name: "stub symlink", link: constants.SystemdResolvePath, contents: stub},
		{name: "regular file naming the stub", contents: stub},
		{name: "srv's bootstrap swap is no verdict", contents: publicBootstrapResolvConf},
		{
			name: "uplink symlink is srv's to fix", link: constants.SystemdResolvedUplinkPath, contents: upstream,
			want: &ResolvedBypassError{Nameserver: "192.168.1.1", Target: constants.SystemdResolvedUplinkPath, Fixable: true},
		},
		{
			name: "another manager's file is not", link: "/run/NetworkManager/resolv.conf", contents: upstream,
			want: &ResolvedBypassError{Nameserver: "192.168.1.1", Target: "/run/NetworkManager/resolv.conf"},
		},
		{
			name: "regular file is not", contents: upstream,
			want: &ResolvedBypassError{Nameserver: "192.168.1.1"},
		},
		{
			name: "NixOS never is", link: constants.SystemdResolvedUplinkPath, contents: upstream, nixos: true,
			want: &ResolvedBypassError{Nameserver: "192.168.1.1", Target: constants.SystemdResolvedUplinkPath, NixOS: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolv, nixos := scratchResolv(t)
			if tc.link != "" {
				if err := os.Symlink(tc.link, resolv); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(resolv, []byte(tc.contents), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.nixos {
				if err := os.WriteFile(nixos, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := resolvedBypass([]byte(tc.contents))
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("bypass = %+v, want none", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Errorf("bypass = %+v, want %+v", got, *tc.want)
			}
		})
	}
}

func TestResolvedBypassFix(t *testing.T) {
	resolv, _ := scratchResolv(t)
	b := &ResolvedBypassError{Nameserver: "192.168.1.1", Target: constants.SystemdResolvedUplinkPath, Fixable: true}
	if want := "sudo ln -sfn " + constants.SystemdResolvePath + " " + resolv; b.Fix() != want {
		t.Errorf("Fix() = %q, want %q", b.Fix(), want)
	}
	if !strings.Contains(b.Error(), b.Fix()) || !strings.Contains(b.Error(), "192.168.1.1") {
		t.Errorf("Error() = %q, want the nameserver and the fix", b.Error())
	}
	nix := &ResolvedBypassError{NixOS: true}
	if !strings.Contains(nix.Fix(), "services.resolved.enable") {
		t.Errorf("NixOS Fix() = %q", nix.Fix())
	}
	var err error = b
	if _, ok := errors.AsType[*ResolvedBypassError](err); !ok {
		t.Error("errors.As does not find the bypass")
	}
}

// RepointResolvConf leaves resolv.conf linked to resolved's stub, and the
// verdict flips to "reaches resolved".
func TestRepointResolvConf(t *testing.T) {
	resolv, _ := scratchResolv(t)
	if err := os.Symlink(constants.SystemdResolvedUplinkPath, resolv); err != nil {
		t.Fatal(err)
	}
	fsShell(t)
	if err := RepointResolvConf(); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(resolv); target != constants.SystemdResolvePath {
		t.Errorf("resolv.conf -> %q, want %q", target, constants.SystemdResolvePath)
	}
}

// The bootstrap restore must undo only srv's own swap: if resolv.conf was
// re-pointed meanwhile (the #13 fix), restoring would put the bypass back.
func TestBootstrapRestoreKeepsLaterRepoint(t *testing.T) {
	resolv, _ := scratchResolv(t)
	uplink := filepath.Join(filepath.Dir(resolv), "uplink")
	if err := os.WriteFile(uplink, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(uplink, resolv); err != nil {
		t.Fatal(err)
	}
	fake := fsShell(t)

	restore, err := EnsureBootstrapResolution()
	if err != nil || restore == nil {
		t.Fatalf("EnsureBootstrapResolution: restore=%t err=%v; want a swap", restore != nil, err)
	}
	stub := filepath.Join(filepath.Dir(resolv), "stub")
	if err := os.WriteFile(stub, []byte("nameserver 127.0.0.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sudoSymlink(stub, resolv); err != nil {
		t.Fatal(err)
	}
	calls := len(fake.Snapshot())
	restore()
	if len(fake.Snapshot()) != calls {
		t.Errorf("restore ran %v after resolv.conf was re-pointed", fake.Snapshot()[calls:])
	}
	if target, _ := os.Readlink(resolv); target != stub {
		t.Errorf("resolv.conf -> %q, want the re-pointed %q", target, stub)
	}
}

// Without an intervening change the restore puts the original symlink back.
func TestBootstrapRestoreRevertsOwnSwap(t *testing.T) {
	resolv, _ := scratchResolv(t)
	orig := filepath.Join(filepath.Dir(resolv), "orig")
	if err := os.WriteFile(orig, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(orig, resolv); err != nil {
		t.Fatal(err)
	}
	fsShell(t)
	restore, err := EnsureBootstrapResolution()
	if err != nil || restore == nil {
		t.Fatalf("EnsureBootstrapResolution: restore=%t err=%v; want a swap", restore != nil, err)
	}
	if data, _ := os.ReadFile(resolv); !isBootstrapResolvConf(string(data)) {
		t.Fatalf("swap not in place: %q", data)
	}
	restore()
	if target, _ := os.Readlink(resolv); target != orig {
		t.Errorf("resolv.conf -> %q, want original %q", target, orig)
	}
}

func TestMDNSInterceptsLocal(t *testing.T) {
	for in, want := range map[string]bool{
		// The host from #13.
		"hosts:          files mdns4_minimal [NOTFOUND=return] mymachines dns\n": true,
		"hosts: files mdns [NOTFOUND=return] dns\n":                              true,
		"hosts: files mdns4_minimal [notfound=return] resolve dns\n":             true,
		// resolve or dns first: DNS is asked before mDNS can stop the lookup.
		"hosts: files resolve [!UNAVAIL=return] mdns4_minimal [NOTFOUND=return] dns\n": false,
		"hosts: files dns mdns4_minimal [NOTFOUND=return]\n":                           false,
		// No stop action: mDNS misses fall through to dns.
		"hosts: files mdns4_minimal dns\n": false,
		// The action belongs to mymachines, not to mdns.
		"hosts: files mdns4_minimal mymachines [NOTFOUND=return] dns\n":          false,
		"# hosts: files mdns4_minimal [NOTFOUND=return] dns\nhosts: files dns\n": false,
		"passwd: files\n": false,
	} {
		if got := mdnsInterceptsLocal(in); got != want {
			t.Errorf("mdnsInterceptsLocal(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSystemResolutionOK(t *testing.T) {
	if !(SystemResolution{}).OK() {
		t.Error("empty resolution should be OK")
	}
	for _, r := range []SystemResolution{
		{Unresolved: []string{"a.test"}},
		{Bypass: &ResolvedBypassError{}},
		{MDNSLocal: []string{"a.local"}},
	} {
		if r.OK() {
			t.Errorf("%+v reported OK", r)
		}
	}
}
