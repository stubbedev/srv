// Package dnsd is srv's embedded DNS server: a UDP responder on the loopback
// that answers the registered local domains from the generated dnsmasq-format
// files and forwards everything else upstream. It replaces the jpillora/dnsmasq
// container — srv was already generating those exact files, the daemon is
// already a resident process, so a container running a second resolver was
// one moving part (and one third-party image) too many.
//
// File formats are unchanged from the dnsmasq era and remain the on-disk
// contract:
//
//	dnsmasq.conf   address=/<name>/<ip>   wildcard entry: <name> and all subdomains
//	               server=<ip[#port]>     upstream forwarders, in order
//	dnsmasq.hosts  <ip> <name>            exact entries (hosts file syntax)
//
// The server re-reads both files on change (fsnotify, debounced) and on
// SIGHUP, so every writer — the daemon in-process, a `srv add` from a
// terminal, a hand edit during debugging — converges without restarts.
package dnsd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	miekg "github.com/miekg/dns"

	"github.com/stubbedev/srv/internal/constants"
)

// CheckName is the health-check query: the server answers it locally with
// 127.0.0.1 before any forwarding, so a successful response proves srv's
// server (not some other resolver that happens to own the port) is listening.
const CheckName = "srv-dns-check.test."

// upstreamTimeout bounds one forward attempt to an upstream server.
const upstreamTimeout = 3 * time.Second

// reloadDebounce coalesces the event burst an atomic rename produces
// (CREATE + RENAME + CHMOD on both files) into one reload.
const reloadDebounce = 250 * time.Millisecond

// pollInterval is how often Watch re-stats the zone files as a safety net
// for change events the platform never delivered.
const pollInterval = time.Second

// zones is the immutable snapshot the handler reads. Reload swaps the pointer;
// queries never block on a mutex.
type zones struct {
	// exact maps a fully-qualified name to its A record.
	exact map[string]string
	// wildcards are suffix matchers: entry "example.com." answers
	// example.com and every subdomain, matching dnsmasq's address=/name/.
	wildcards []wildcard
	// upstream holds the forwarder addresses in config order, "ip#port"
	// already split into host/port.
	upstream []upstream
}

type wildcard struct {
	suffix string // lower-case FQDN, leading dot included: ".example.com."
	ip     string
}

type upstream struct {
	host string
	port int
}

// Server is the embedded DNS responder.
type Server struct {
	conn      *miekg.Server
	tcp       *miekg.Server
	zones     atomic.Pointer[zones]
	confPath  string
	hostsPath string

	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	done      chan struct{}
}

// New creates a server bound to bindAddr:port without serving yet. The zone
// files are loaded eagerly so a broken file fails fast at startup rather than
// on the first query.
func New(bindAddr string, port int, confPath, hostsPath string) (*Server, error) {
	if _, err := loadZones(confPath, hostsPath); err != nil {
		return nil, fmt.Errorf("load DNS zones: %w", err)
	}

	udpAddr := &net.UDPAddr{IP: net.ParseIP(bindAddr), Port: port}
	listenCfg := &net.ListenConfig{}
	pc, err := listenCfg.ListenPacket(context.Background(), "udp", udpAddr.String())
	if err != nil {
		return nil, fmt.Errorf("bind DNS %s: %w", udpAddr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		confPath:  confPath,
		hostsPath: hostsPath,
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	mux := miekg.NewServeMux()
	mux.HandleFunc(".", s.handleQuery)
	s.conn = &miekg.Server{PacketConn: pc, Handler: mux}
	ln, err := net.Listen("tcp", udpAddr.String())
	if err != nil {
		_ = pc.Close()
		cancel()
		return nil, fmt.Errorf("bind DNS tcp %s: %w", udpAddr, err)
	}
	s.tcp = &miekg.Server{Listener: ln, Handler: mux}
	if err := s.Reload(); err != nil {
		_ = pc.Close()
		_ = ln.Close()
		cancel()
		return nil, err
	}
	return s, nil
}

// Addr reports the bound UDP address (useful when the caller passed port 0).
func (s *Server) Addr() string {
	return s.conn.PacketConn.LocalAddr().String()
}

// Ping probes the server on the loopback at the embedded port with the
// health-check name. True
// only when the answer is srv's own loopback response — which is what makes
// it a usable "is our DNS running" check from doctor and the lifecycle code.
func Ping() bool {
	return pingAddr(net.JoinHostPort(constants.LocalhostIP, constants.PortDNSStr))
}

// PingAddr probes a specific server; tests point it at an ephemeral port.
func PingAddr(addr string) bool {
	return pingAddr(addr)
}

func pingAddr(addr string) bool {
	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn(CheckName), miekg.TypeA)
	m.RecursionDesired = true

	client := &miekg.Client{Timeout: 2 * time.Second, Net: "udp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil || resp.Rcode != miekg.RcodeSuccess {
		return false
	}
	for _, rr := range resp.Answer {
		if a, ok := rr.(*miekg.A); ok && a.A.String() == constants.LocalhostIP {
			return true
		}
	}
	return false
}

// Serve blocks serving queries until Shutdown. The returned error is nil on
// graceful shutdown.
func (s *Server) Serve() error {
	tcpDone := make(chan struct{})
	go func() {
		defer close(tcpDone)
		if err := s.tcp.ActivateAndServe(); err != nil && s.ctx.Err() == nil {
			log.Printf("dnsd: tcp serve: %v", err)
		}
	}()
	err := s.conn.ActivateAndServe()
	_ = s.tcp.ShutdownContext(context.Background())
	<-tcpDone
	close(s.done)
	if s.ctx.Err() != nil {
		// Shutdown in progress: any socket error here is the expected close
		// noise, and callers read nil as "stopped on request".
		return nil //nolint:nilerr // deliberate: cancelled ctx converts to a clean stop
	}
	return err
}

// Shutdown stops the server.
func (s *Server) Shutdown() {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.conn.ShutdownContext(context.Background())
		_ = s.tcp.ShutdownContext(context.Background())
	})
	<-s.done
}

type fileStamp struct {
	size int64
	mod  time.Time
}

func fileStampOf(path string) (fileStamp, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{size: fi.Size(), mod: fi.ModTime()}, true
}

// Watch reloads the zone files whenever they change, until ctx is done. It
// blocks; run it in a goroutine. The reload immediately after the watchers
// are installed closes the startup race: a zone file rewritten between New
// and Add produces events nobody was watching for, so the watcher's first
// act is to sync once from disk.
func (s *Server) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("DNS watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	// Atomic writes replace the file, so watch the directories, not the
	// files: watching an inode that gets renamed away watches nothing.
	dirs := map[string]bool{}
	for _, p := range []string{s.confPath, s.hostsPath} {
		dirs[filepath.Dir(p)] = true
	}
	for dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			return fmt.Errorf("watch %s: %w", dir, err)
		}
	}
	if err := s.Reload(); err != nil {
		// Startup state still matters; surface it but keep serving.
		log.Printf("dnsd: initial reload failed: %v", err)
	}

	debounce := time.NewTimer(reloadDebounce)
	if !debounce.Stop() {
		<-debounce.C
	}
	pending := false
	confStamp, _ := fileStampOf(s.confPath)
	hostsStamp, _ := fileStampOf(s.hostsPath)
	markDirty := func() {
		if !pending {
			pending = true
			debounce.Reset(reloadDebounce)
		}
	}
	refresh := func() {
		if err := s.Reload(); err != nil {
			// A half-written or hand-mangled file must not kill the server;
			// keep the last good zones and surface the problem.
			log.Printf("dnsd: reload failed, keeping previous zones: %v", err)
			return
		}
		confStamp, _ = fileStampOf(s.confPath)
		hostsStamp, _ = fileStampOf(s.hostsPath)
	}
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			name := filepath.Base(event.Name)
			if name != filepath.Base(s.confPath) && name != filepath.Base(s.hostsPath) {
				continue
			}
			markDirty()
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("dnsd: watch error: %v", err)
		case <-poll.C:
			if cur, ok := fileStampOf(s.confPath); ok && cur != confStamp {
				confStamp = cur
				markDirty()
			}
			if cur, ok := fileStampOf(s.hostsPath); ok && cur != hostsStamp {
				hostsStamp = cur
				markDirty()
			}
		case <-debounce.C:
			if pending {
				pending = false
				refresh()
			}
		}
	}
}

// Reload re-reads both zone files and swaps the serving snapshot. The server
// keeps answering from the previous snapshot while the files are read.
func (s *Server) Reload() error {
	z, err := loadZones(s.confPath, s.hostsPath)
	if err != nil {
		return err
	}
	s.zones.Store(z)
	return nil
}

// loadZones parses the dnsmasq-format conf and hosts files into a zone
// snapshot. Missing files yield empty zones (a fresh install serves nothing
// but the health check and upstream forwarding); malformed lines inside an
// otherwise parseable file are skipped so one bad line cannot take DNS down.
func loadZones(confPath, hostsPath string) (*zones, error) {
	z := &zones{exact: map[string]string{}}

	if data, err := os.ReadFile(hostsPath); err == nil {
		parseHosts(string(data), z)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", hostsPath, err)
	}

	if data, err := os.ReadFile(confPath); err == nil {
		parseConf(string(data), z)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", confPath, err)
	}

	if len(z.upstream) == 0 {
		z.upstream = defaultUpstream()
	}
	return z, nil
}

// parseHosts reads hosts-file lines: "<ip> <name> [alias...]" — every name
// on the line is registered, as dnsmasq does. Only entries pointing at the
// loopback are kept: srv's registry never contains anything else, and
// silently becoming an A record for a foreign host entry would be surprising.
func parseHosts(data string, z *zones) {
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != constants.LocalhostIP {
			continue
		}
		for _, name := range fields[1:] {
			z.exact[strings.ToLower(dnsName(name))] = constants.LocalhostIP
		}
	}
}

// parseConf reads the generated dnsmasq.conf: address=/name/ip (wildcard:
// name and every subdomain), server=ip[#port] upstreams. Unrecognized lines
// are skipped.
func parseConf(data string, z *zones) {
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "address=/"):
			rest := strings.TrimPrefix(line, "address=/")
			name, ip, found := strings.Cut(rest, "/")
			if !found || name == "" || ip == "" {
				continue
			}
			z.wildcards = append(z.wildcards, wildcard{
				suffix: "." + strings.ToLower(dnsName(name)),
				ip:     ip,
			})
		case strings.HasPrefix(line, "server="):
			srv := strings.TrimPrefix(line, "server=")
			host, port, hasPort := strings.Cut(srv, "#")
			p := 53
			if hasPort {
				n, err := strconv.Atoi(port)
				if err != nil || n < 1 || n > 65535 {
					continue
				}
				p = n
			}
			if net.ParseIP(host) == nil {
				continue
			}
			z.upstream = append(z.upstream, upstream{host: host, port: p})
		}
	}
}

// defaultUpstream is used when the conf declares no server= lines: the same
// Google resolvers srv writes into a fresh conf.
func defaultUpstream() []upstream {
	return []upstream{
		{host: constants.GoogleDNS1, port: 53},
		{host: constants.GoogleDNS2, port: 53},
	}
}

// dnsName qualifies and normalizes a config name for map keys.
func dnsName(name string) string {
	name = strings.TrimSuffix(name, ".")
	return name + "."
}

// handleQuery answers one DNS query.
func (s *Server) handleQuery(w miekg.ResponseWriter, r *miekg.Msg) {
	m := new(miekg.Msg)
	m.SetReply(r)
	m.RecursionAvailable = true
	q := r.Question[0]
	name := strings.ToLower(q.Name)

	// The health-check name is answered before everything else, including
	// forwarding, so a response to it is proof srv's server answered.
	if name == CheckName && q.Qtype == miekg.TypeA {
		appendA(m, name, constants.LocalhostIP)
		_ = w.WriteMsg(m)
		return
	}

	z := s.zones.Load()
	if z == nil {
		miekg.HandleFailed(w, r)
		return
	}

	switch q.Qtype {
	case miekg.TypeA:
		if ip, ok := lookupA(z, name); ok {
			appendA(m, name, ip)
			_ = w.WriteMsg(m)
			return
		}
	case miekg.TypeAAAA:
		// Local domains are IPv4-only: an authoritative empty answer (not
		// NXDOMAIN, which would push resolvers to try upstream) matches what
		// dnsmasq served.
		if _, ok := lookupA(z, name); ok {
			_ = w.WriteMsg(m)
			return
		}
	default:
		// MX/TXT/SRV for local names: empty authoritative answer, like
		// dnsmasq with no matching record type.
		if _, ok := lookupA(z, name); ok {
			_ = w.WriteMsg(m)
			return
		}
	}

	forwardUpstream(w, r, m, z)
}

// lookupA resolves one name against the snapshot: exact entries first, then
// wildcard suffixes (dnsmasq semantics: address=/name/ covers the apex and
// every subdomain).
func lookupA(z *zones, name string) (string, bool) {
	if ip, ok := z.exact[name]; ok {
		return ip, true
	}
	for _, w := range z.wildcards {
		// suffix ".example.com." matches "example.com." (apex) after the
		// leading dot is accounted for, and every "x.example.com.".
		if name == strings.TrimPrefix(w.suffix, ".") || strings.HasSuffix(name, w.suffix) {
			return w.ip, true
		}
	}
	return "", false
}

func appendA(m *miekg.Msg, name, ip string) {
	m.Answer = append(m.Answer, &miekg.A{
		Hdr: miekg.RR_Header{Name: name, Rrtype: miekg.TypeA, Class: miekg.ClassINET, Ttl: 60},
		A:   net.ParseIP(ip),
	})
}

// forwardUpstream relays the original question to the configured servers all
// at once; the first response wins. The worst case is one upstreamTimeout no
// matter how many servers are configured or how many are dead — a healthy
// upstream's answer never waits behind a dead one's timeout.
func forwardUpstream(w miekg.ResponseWriter, r *miekg.Msg, reply *miekg.Msg, z *zones) {
	client := &miekg.Client{Timeout: upstreamTimeout, Net: "udp"}
	replies := make(chan *miekg.Msg, len(z.upstream))
	for _, up := range z.upstream {
		go func(up upstream) {
			defer func() { replies <- nil }()
			addr := net.JoinHostPort(up.host, strconv.Itoa(up.port))
			resp, _, err := client.Exchange(r.Copy(), addr)
			if err == nil {
				replies <- resp
			}
		}(up)
	}
	for range z.upstream {
		if resp := <-replies; resp != nil {
			_ = w.WriteMsg(resp)
			return
		}
	}
	// No upstream answered. For a name srv owns this is unreachable (lookupA
	// matched); for anything else the honest answer is SERVFAIL rather than a
	// forged empty answer for a name we know nothing about.
	reply.Rcode = miekg.RcodeServerFailure
	reply.Answer = nil
	_ = w.WriteMsg(reply)
}

// IsBindPermissionErr reports whether err is the classic "cannot bind port
// < 1024 as an unprivileged user" failure, so callers can print the exact fix
// instead of a raw syscall error.
func IsBindPermissionErr(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}
