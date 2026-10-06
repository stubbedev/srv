// Package dnsd is srv's embedded DNS server: a UDP responder on the loopback
// that answers the registered local domains from the generated dnsmasq-format
// files and forwards everything else upstream. It replaces the jpillora/dnsmasq
// container — srv was already generating those exact files, the daemon is
// already a resident process, so a container running a second resolver was
// one moving part (and one third-party image) too many.
//
// File formats are unchanged from the dnsmasq era and remain the on-disk
// fallback contract:
//
//	dnsmasq.conf   address=/<name>/<ip>   wildcard entry: <name> and all subdomains
//	               server=<ip[#port]>     upstream forwarders, in order
//	dnsmasq.hosts  <ip> <name>            exact entries (hosts file syntax)
//
// Zones reach the server through two layers. The primary layer is a snapshot
// built from the structured config — the site yaml data and the local-domains
// registry — that the daemon pushes in-process via SetZones, so a
// registration answers without waiting for a file render. The fallback layer
// is the two generated files, parsed on start, on change (fsnotify,
// debounced) and on SIGHUP; it keeps the escape hatch of hand-written
// entries during debugging working. Queries consult the primary layer first
// and fall through to file entries the structured config does not know.
package dnsd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
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
	"github.com/stubbedev/srv/internal/domain"
	"github.com/stubbedev/srv/internal/pool"
)

// CheckName is the health-check query: the server answers it locally with
// 127.0.0.1 before any forwarding, so a successful response proves srv's
// server (not some other resolver that happens to own the port) is listening.
const CheckName = "srv-dns-check.test."

// upstreamTimeout bounds one forward attempt to an upstream server.
const upstreamTimeout = 3 * time.Second

// shutdownGrace bounds every wait in Shutdown: graceful first, then sockets
// are force-closed. A wedged serve loop or handler must never stall daemon
// stop (or a failing test's cleanup) past this bound.
const shutdownGrace = 2 * time.Second

// reloadDebounce coalesces the event burst an atomic rename produces
// (CREATE + RENAME + CHMOD on both files) into one reload.
const reloadDebounce = 250 * time.Millisecond

// pollInterval is how often Watch re-stats the zone files as a safety net
// for change events the platform never delivered. fsnotify is the primary
// signal, so the net is coarse: a resident daemon should not wake every
// second to stat files that almost never change. After a watcher error the
// events can no longer be trusted, and Watch drops to pollIntervalDegraded.
const (
	pollInterval         = 30 * time.Second
	pollIntervalDegraded = time.Second
)

// fileWatcher is the fsnotify surface Watch depends on. Tests substitute a
// synchronous fake so platform event latency never decides a test outcome.
type fileWatcher interface {
	Add(dir string) error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Close() error
}

// fsnotifyWatcher adapts *fsnotify.Watcher, whose Events and Errors are
// channels rather than methods, to fileWatcher.
type fsnotifyWatcher struct{ *fsnotify.Watcher }

func (w fsnotifyWatcher) Events() <-chan fsnotify.Event { return w.Watcher.Events }
func (w fsnotifyWatcher) Errors() <-chan error          { return w.Watcher.Errors }

// ZoneSnapshot is one immutable set of zones. The handler reads the combined
// snapshot through an atomic pointer: SetZones and Reload swap the pointer,
// queries never block on a mutex.
//
// Addresses are parsed once, when the snapshot is built, into the 4-byte
// form an A record packs; answering a query copies a slice header.
type ZoneSnapshot struct {
	// exact maps a lower-case FQDN to its A record.
	exact map[string]net.IP
	// wildcards maps a lower-case FQDN ("example.com.") to the A record for
	// it and every subdomain, matching dnsmasq's address=/name/. Lookups walk
	// the query name's labels, so cost scales with name depth, not with the
	// number of registered domains; the most specific entry wins.
	wildcards map[string]net.IP
	// upstream holds the forwarder addresses in config order, "ip#port"
	// already split into host/port.
	upstream []upstream
}

type upstream struct {
	host string
	port int
	// addr is host:port, joined once for every forwarded query.
	addr string
}

// loopbackA is the answer for srv's own names and the health check.
var loopbackA = net.ParseIP(constants.LocalhostIP).To4()

// parseA parses an A-record address: IPv4 only, in 4-byte form. Anything
// else is nil, so a snapshot can never hold a record that fails to pack.
func parseA(ip string) net.IP {
	return net.ParseIP(ip).To4()
}

// NewZoneSnapshot returns an empty snapshot for callers that build zones
// from the structured config (the daemon feeds these via Server.SetZones).
// Upstreams are left unset: an empty upstream list defers to the file layer
// and its defaults, so only a resolver list explicitly configured in
// config.yml overrides what the files declare.
func NewZoneSnapshot() *ZoneSnapshot {
	return newZoneSnapshot()
}

func newZoneSnapshot() *ZoneSnapshot {
	return &ZoneSnapshot{exact: map[string]net.IP{}, wildcards: map[string]net.IP{}}
}

// PinExact adds an A record matching name exactly — the structured twin of
// an "<ip> <name>" hosts-file line. Entries with an invalid IPv4 address are
// dropped so a snapshot can never hold a record the query path cannot
// serve.
func (z *ZoneSnapshot) PinExact(name, ip string) {
	if a := parseA(ip); a != nil && name != "" {
		z.exact[strings.ToLower(dnsName(name))] = a
	}
}

// PinWildcard answers name and every subdomain — the structured twin of
// dnsmasq's address=/name/ directive that parseConf renders from the files.
// The first declaration of a name wins, as in the config files.
func (z *ZoneSnapshot) PinWildcard(name, ip string) {
	a := parseA(ip)
	if a == nil || name == "" {
		return
	}
	key := strings.ToLower(dnsName(name))
	if _, taken := z.wildcards[key]; !taken {
		z.wildcards[key] = a
	}
}

// SetUpstream registers one forwarder, "ip" or "ip#port" — the same grammar
// as dnsmasq's server= lines in the fallback files. Reports whether the spec
// was valid; invalid entries are dropped.
func (z *ZoneSnapshot) SetUpstream(spec string) bool {
	up, ok := parseUpstreamSpec(spec)
	if !ok {
		return false
	}
	z.upstream = append(z.upstream, up)
	return true
}

// Server is the embedded DNS responder.
type Server struct {
	conn *miekg.Server
	tcp  *miekg.Server
	// zones is the combined serving snapshot; primary is the structured
	// feed installed by SetZones, fallback the parsed zone files.
	zones     atomic.Pointer[ZoneSnapshot]
	primary   atomic.Pointer[ZoneSnapshot]
	fallback  atomic.Pointer[ZoneSnapshot]
	confPath  string
	hostsPath string

	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	done      chan struct{}

	// updMu serializes SetZones against Reload so an interleave cannot
	// publish a snapshot built from one stale layer; queries keep their
	// lock-free atomic load of the combined snapshot.
	updMu sync.Mutex

	// newFileWatcher builds the watcher Watch runs on; pollEvery bounds its
	// stat-poll safety net. Both default to production values and are
	// tightened by tests, which assert the bound the code owns instead of
	// the platform's event latency.
	newFileWatcher func() (fileWatcher, error)
	pollEvery      time.Duration

	// loopReady closes once watchLoop has finished its initial sync and its
	// poll ticker is live, so a test can order its file writes after the
	// watcher is fully up instead of racing Watch's startup.
	loopReady     chan struct{}
	loopReadyOnce sync.Once
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
		newFileWatcher: func() (fileWatcher, error) {
			w, err := fsnotify.NewWatcher()
			if err != nil {
				return nil, err
			}
			return fsnotifyWatcher{w}, nil
		},
		pollEvery: pollInterval,
		loopReady: make(chan struct{}),
	}
	mux := miekg.NewServeMux()
	mux.HandleFunc(".", s.handleQuery)
	// UDPSize is both the inbound read buffer and the size advertised in
	// srv's EDNS0 replies. The 512 default cannot carry today's routine
	// 1232-4096 byte EDNS payloads, so larger queries failed to parse.
	s.conn = &miekg.Server{PacketConn: pc, Handler: mux, UDPSize: serverUDPSize}
	ln, err := listenCfg.Listen(context.Background(), "tcp", udpAddr.String())
	if err != nil {
		_ = pc.Close()
		cancel()
		return nil, fmt.Errorf("bind DNS tcp %s: %w", udpAddr, err)
	}
	s.tcp = &miekg.Server{Listener: ln, Handler: mux, UDPSize: serverUDPSize}
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
	tcpCtx, tcpCancel := context.WithTimeout(context.Background(), shutdownGrace)
	_ = s.tcp.ShutdownContext(tcpCtx)
	tcpCancel()
	select {
	case <-tcpDone:
	case <-time.After(shutdownGrace):
	}
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
		udpCtx, udpCancel := context.WithTimeout(context.Background(), shutdownGrace)
		_ = s.conn.ShutdownContext(udpCtx)
		udpCancel()
		tcpCtx, tcpCancel := context.WithTimeout(context.Background(), shutdownGrace)
		_ = s.tcp.ShutdownContext(tcpCtx)
		tcpCancel()
	})
	select {
	case <-s.done:
	case <-time.After(shutdownGrace):
		// The graceful path did not unwind Serve (a wedged handler or serve
		// loop): closing the sockets makes ActivateAndServe return, and Serve
		// then closes done on its own.
		_ = s.conn.PacketConn.Close()
		_ = s.tcp.Listener.Close()
		select {
		case <-s.done:
		case <-time.After(shutdownGrace):
		}
	}
}

// Watch reloads the fallback zone files whenever they change, until ctx is
// done. It blocks; run it in a goroutine. The reload immediately after the
// watchers are installed closes the startup race: a zone file rewritten
// between New and Add produces events nobody was watching for, so the
// watcher's first act is to sync once from disk.
func (s *Server) Watch() error {
	watcher, err := s.newFileWatcher()
	if err != nil {
		return fmt.Errorf("DNS watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	// Atomic writes replace the file, so watch the directories, not the
	// files: watching an inode that gets renamed away watches nothing.
	confDir, hostsDir := filepath.Dir(s.confPath), filepath.Dir(s.hostsPath)
	if err := watcher.Add(confDir); err != nil {
		return fmt.Errorf("watch %s: %w", confDir, err)
	}
	if hostsDir != confDir {
		if err := watcher.Add(hostsDir); err != nil {
			return fmt.Errorf("watch %s: %w", hostsDir, err)
		}
	}
	return s.watchLoop(watcher.Events(), watcher.Errors())
}

// SetZones installs a snapshot built from the structured config as the
// primary layer — the daemon pushes one on start and whenever the
// local-domains registry, a DNS-alias redirect, or config.yml changes — and
// swaps the serving snapshot. The file-derived layer stays loaded beneath
// it, so hand-written zone entries keep answering and a primary that no
// longer lists a name falls back to whatever the files still say.
func (s *Server) SetZones(z *ZoneSnapshot) {
	// Serialize with Reload: both rebuild the serving snapshot from one
	// primary and one fallback layer, and an unlocked interleave (Reload
	// pairing the old primary, SetZones the old fallback) would publish a
	// snapshot that silently reverts one layer until the next event.
	s.updMu.Lock()
	defer s.updMu.Unlock()
	s.primary.Store(z)
	s.zones.Store(combine(z, s.fallback.Load()))
}

// Reload re-reads both zone files into the fallback layer and swaps the
// serving snapshot. The server keeps answering from the previous snapshot
// while the files are read.
func (s *Server) Reload() error {
	z, err := loadZones(s.confPath, s.hostsPath)
	if err != nil {
		return err
	}
	s.updMu.Lock()
	defer s.updMu.Unlock()
	s.fallback.Store(z)
	s.zones.Store(combine(s.primary.Load(), z))
	return nil
}

// watchLoop is Watch's event loop, fed by the watcher's channels.
//
// An event is only a hint that something in a watched directory moved; which
// ops a backend reports for an atomic rename-over is not dependable. kqueue
// (macOS, BSD) reports the replaced file as Remove and emits the matching
// Create only when its directory rescan happens to run after the file's
// delete note, so a filter on Create/Write/Rename missed the rewrite
// intermittently. Every event therefore just schedules a debounced stat of
// both files, and the files reload only when a stamp actually differs: the
// on-disk state decides, never the event type or name. Unrelated churn in
// the directories costs two stats per debounce window and no reload.
func (s *Server) watchLoop(events <-chan fsnotify.Event, errs <-chan error) error {
	// Stat before reading: a write landing between the two then leaves a
	// stale stamp that the next check catches, rather than a fresh stamp
	// over stale zones.
	stamps := s.statZones()
	if err := s.Reload(); err != nil {
		// Startup state still matters; surface it but keep serving.
		log.Printf("dnsd: initial reload failed: %v", err)
	}

	debounce := time.NewTimer(reloadDebounce)
	if !debounce.Stop() {
		<-debounce.C
	}
	pending := false
	markDirty := func() {
		if !pending {
			pending = true
			debounce.Reset(reloadDebounce)
		}
	}
	poll := time.NewTicker(s.pollEvery)
	defer poll.Stop()
	s.loopReadyOnce.Do(func() { close(s.loopReady) })
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case _, ok := <-events:
			if !ok {
				return nil
			}
			markDirty()
		case err, ok := <-errs:
			if !ok {
				return nil
			}
			log.Printf("dnsd: watch error: %v", err)
			// An overflowed or failing watcher may have dropped events:
			// lean on the stat poll from here on.
			poll.Reset(min(pollIntervalDegraded, s.pollEvery))
		case <-poll.C:
			// The safety net for events the platform never delivered.
			markDirty()
		case <-debounce.C:
			pending = false
			cur := s.statZones()
			if cur.equal(stamps) {
				continue
			}
			if err := s.Reload(); err != nil {
				// A half-written or hand-mangled file must not kill the
				// server; keep the last good zones and the old stamps, so
				// the next event or poll retries.
				log.Printf("dnsd: reload failed, keeping previous zones: %v", err)
				continue
			}
			stamps = cur
		}
	}
}

// zoneStamps identifies the on-disk version of both zone files: which file
// each name points at (os.SameFile, so a rename-over is a change even when
// size and mtime happen to match) plus its size and mtime. A missing file is
// a nil entry, so a deletion is a change too — the reload then drops that
// file's records, which is what an absent file means to loadZones.
type zoneStamps [2]os.FileInfo

func (s *Server) statZones() zoneStamps {
	var st zoneStamps
	for i, p := range [2]string{s.confPath, s.hostsPath} {
		if fi, err := os.Stat(p); err == nil {
			st[i] = fi
		}
	}
	return st
}

func (a zoneStamps) equal(b zoneStamps) bool {
	for i := range a {
		x, y := a[i], b[i]
		if x == nil || y == nil {
			if x != y {
				return false
			}
			continue
		}
		if !os.SameFile(x, y) || x.Size() != y.Size() || !x.ModTime().Equal(y.ModTime()) {
			return false
		}
	}
	return true
}

// combine overlays the structured snapshot on the file-derived one and
// returns the snapshot queries are served from. Primary entries win per
// name; file-only entries stay visible underneath. Primary upstreams apply
// only when the structured config explicitly declares some, so hand-written
// server= lines keep working while config.yml names none.
func combine(primary, fallback *ZoneSnapshot) *ZoneSnapshot {
	if primary == nil {
		return fallback
	}
	if fallback == nil {
		return primary
	}
	merged := &ZoneSnapshot{
		exact:     make(map[string]net.IP, len(primary.exact)+len(fallback.exact)),
		wildcards: make(map[string]net.IP, len(primary.wildcards)+len(fallback.wildcards)),
	}
	maps.Copy(merged.exact, fallback.exact)
	maps.Copy(merged.exact, primary.exact)
	maps.Copy(merged.wildcards, fallback.wildcards)
	maps.Copy(merged.wildcards, primary.wildcards)
	merged.upstream = primary.upstream
	if len(merged.upstream) == 0 {
		merged.upstream = fallback.upstream
	}
	return merged
}

// loadZones parses the dnsmasq-format conf and hosts files into a zone
// snapshot. Missing files yield empty zones (a fresh install serves nothing
// but the health check and upstream forwarding); malformed lines inside an
// otherwise parseable file are skipped so one bad line cannot take DNS down.
func loadZones(confPath, hostsPath string) (*ZoneSnapshot, error) {
	z := newZoneSnapshot()

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
func parseHosts(data string, z *ZoneSnapshot) {
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
			z.exact[strings.ToLower(dnsName(name))] = loopbackA
		}
	}
}

// parseConf reads the generated dnsmasq.conf: address=/name/ip (wildcard:
// name and every subdomain), server=ip[#port] upstreams. Unrecognized lines
// are skipped.
func parseConf(data string, z *ZoneSnapshot) {
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
			z.PinWildcard(name, ip)
		case strings.HasPrefix(line, "server="):
			if up, ok := parseUpstreamSpec(strings.TrimPrefix(line, "server=")); ok {
				z.upstream = append(z.upstream, up)
			}
		}
	}
}

// parseUpstreamSpec splits a "host[#port]" forwarder spec, the grammar of
// dnsmasq's server= lines and of config.yml's upstream_dns entries. The port
// defaults to 53.
func parseUpstreamSpec(spec string) (upstream, bool) {
	host, port, hasPort := strings.Cut(spec, "#")
	p := 53
	if hasPort {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return upstream{}, false
		}
		p = n
	}
	if net.ParseIP(host) == nil {
		return upstream{}, false
	}
	return newUpstream(host, p), true
}

// defaultUpstream is used when the conf declares no server= lines: the same
// Google resolvers srv writes into a fresh conf.
func defaultUpstream() []upstream {
	return []upstream{
		newUpstream(constants.GoogleDNS1, 53),
		newUpstream(constants.GoogleDNS2, 53),
	}
}

// newUpstream is the only way to build an upstream, so addr always matches
// host and port.
func newUpstream(host string, port int) upstream {
	return upstream{host: host, port: port, addr: net.JoinHostPort(host, strconv.Itoa(port))}
}

// dnsName qualifies and normalizes a config name for map keys.
func dnsName(name string) string {
	name = strings.TrimSuffix(name, ".")
	return name + "."
}

// reply is the per-query response, pooled: the message, its one A record and
// the answer section backing it. Reuse is safe because the ResponseWriter
// packs the message to bytes inside WriteMsg and keeps no reference.
type reply struct {
	msg    miekg.Msg
	a      miekg.A
	answer [1]miekg.RR
}

var replies pool.Pool[reply]

// newReply starts the response to r: miekg's SetReply, except the question
// section aliases the request's instead of copying it (both are read-only
// until the reply is packed) — no allocation per query.
func newReply(r *miekg.Msg) *reply {
	rp := replies.Get()
	rp.msg = miekg.Msg{}
	m := &rp.msg
	m.Id = r.Id
	m.Response = true
	m.Opcode = r.Opcode
	if m.Opcode == miekg.OpcodeQuery {
		m.RecursionDesired = r.RecursionDesired
		m.CheckingDisabled = r.CheckingDisabled
	}
	m.Rcode = miekg.RcodeSuccess
	m.RecursionAvailable = true
	m.Question = r.Question[:1:1]
	return rp
}

// answerA sets the reply's answer section to one A record.
func (rp *reply) answerA(name string, ip net.IP) {
	rp.a = miekg.A{
		Hdr: miekg.RR_Header{Name: name, Rrtype: miekg.TypeA, Class: miekg.ClassINET, Ttl: 60},
		A:   ip,
	}
	rp.answer[0] = &rp.a
	rp.msg.Answer = rp.answer[:1]
}

// release returns the reply to the pool once it has been written.
func (rp *reply) release() {
	rp.msg = miekg.Msg{}
	rp.a = miekg.A{}
	rp.answer[0] = nil
	replies.Put(rp)
}

// handleQuery answers one DNS query. A local answer allocates nothing in
// this package; the remaining cost is miekg packing the message.
func (s *Server) handleQuery(w miekg.ResponseWriter, r *miekg.Msg) {
	rp := newReply(r)
	defer rp.release()
	q := r.Question[0]
	name := strings.ToLower(q.Name)

	// The health-check name is answered before everything else, including
	// forwarding, so a response to it is proof srv's server answered.
	if name == CheckName && q.Qtype == miekg.TypeA {
		// The answer's owner name echoes the question's case (RFC 1035
		// 4.1.9); lookups key on the lowercased name.
		rp.answerA(q.Name, loopbackA)
		writeReply(w, r, &rp.msg)
		return
	}

	z := s.zones.Load()
	if z == nil {
		miekg.HandleFailed(w, r)
		return
	}

	if ip, ok := lookupA(z, name); ok {
		// Local names are IPv4-only. Any other type (AAAA, MX, TXT, …) gets
		// an authoritative empty answer, not NXDOMAIN, which would push
		// resolvers to try upstream — what dnsmasq served.
		if q.Qtype == miekg.TypeA {
			rp.answerA(q.Name, ip)
		}
		writeReply(w, r, &rp.msg)
		return
	}

	forwardUpstream(w, r, &rp.msg, z)
}

// lookupA resolves one name against the snapshot: exact entries first, then
// wildcard entries (dnsmasq semantics: address=/name/ covers the apex and
// every subdomain), the most specific winning.
func lookupA(z *ZoneSnapshot, name string) (net.IP, bool) {
	if ip, ok := z.exact[name]; ok {
		return ip, true
	}
	return domain.MatchSuffix(z.wildcards, name)
}

// upstreamClient is shared by every forwarded query: a Client holds only
// settings, and Exchange dials a fresh socket per call, so one instance is
// safe for concurrent use and saves an allocation per query.
var upstreamClient = &miekg.Client{Timeout: upstreamTimeout, Net: "udp"}

// upstreamTCP retries a truncated forwarder answer over TCP, the only
// transport that can carry it (see forwardUpstream).
var upstreamTCP = &miekg.Client{Timeout: upstreamTimeout, Net: "tcp"}

// serverUDPSize is srv's own EDNS0 buffer: the inbound UDP read size and
// the value advertised in replies.
const serverUDPSize = 4096

// upstreamAnswer is the hedged race's winner: the reply and the upstream
// that produced it, so a truncated answer can be retried over TCP against
// the same resolver.
type upstreamAnswer struct {
	resp *miekg.Msg
	addr string
}

// writeReply sends m to the client. The request's OPT record is echoed with
// srv's own buffer so EDNS0 size negotiation keeps working through srv, and
// over UDP the message is truncated to the buffer the client advertised
// (512 without EDNS0) with the TC bit — miekg's WriteMsg never truncates on
// its own, and an oversized datagram is simply dropped by the client.
func writeReply(w miekg.ResponseWriter, r *miekg.Msg, m *miekg.Msg) {
	size := miekg.MinMsgSize
	if o := r.IsEdns0(); o != nil {
		if s := int(o.UDPSize()); s > size {
			size = s
		}
		// Replace any OPT the upstream sent with srv's own.
		for i, rr := range m.Extra {
			if _, ok := rr.(*miekg.OPT); ok {
				m.Extra = append(m.Extra[:i:i], m.Extra[i+1:]...)
				break
			}
		}
		m.SetEdns0(serverUDPSize, o.Do())
	}
	if w.LocalAddr().Network() == "udp" {
		m.Truncate(size)
	}
	_ = w.WriteMsg(m)
}

// hedgeDelay is how long forwardUpstream waits on one upstream before it
// also asks the next. A healthy resolver answers well inside it, so the
// common case sends one packet instead of one per configured upstream.
const hedgeDelay = 200 * time.Millisecond

// forwardUpstream relays the original question upstream (see exchangeUpstream)
// and writes the winning answer, or SERVFAIL when no upstream answered.
func forwardUpstream(w miekg.ResponseWriter, r *miekg.Msg, reply *miekg.Msg, z *ZoneSnapshot) {
	ans := exchangeUpstream(r, z.upstream)
	if ans.resp != nil {
		resp := ans.resp
		// A truncated forwarder answer must be re-asked over TCP. Passing
		// it through unchanged meant the client's own TCP retry was
		// forwarded upstream over UDP again, got TC again, and large
		// answers (DNSSEC chains, long TXT records) could never be resolved
		// through srv.
		if resp.Truncated {
			if overTCP, _, err := upstreamTCP.Exchange(r.Copy(), ans.addr); err == nil {
				resp = overTCP
			}
		}
		writeReply(w, r, resp)
		return
	}
	// No upstream answered. For a name srv owns this is unreachable (lookupA
	// matched); for anything else the honest answer is SERVFAIL rather than a
	// forged empty answer for a name we know nothing about.
	reply.Rcode = miekg.RcodeServerFailure
	reply.Answer = nil
	writeReply(w, r, reply)
}

// exchangeUpstream asks the upstreams as a hedged race: the first upstream is
// asked at once, the next one joins when the previous has failed or stayed
// silent for hedgeDelay, and the first answer wins. A healthy first upstream
// therefore costs one query, and a dead one costs at most hedgeDelay before
// the next is tried — never a full upstreamTimeout. The zero value (nil
// resp) means none answered.
//
// Each worker sends exactly one message (its answer, or the zero value on
// failure) into a channel buffered for all of them, so the losers of the
// race finish and exit after the winner is returned: no worker may ever
// block on the send, or every forwarded query would leak a goroutine per
// slow upstream.
func exchangeUpstream(r *miekg.Msg, upstreams []upstream) upstreamAnswer {
	replies := make(chan upstreamAnswer, len(upstreams))
	next, inflight := 0, 0
	launch := func() {
		up := upstreams[next]
		next++
		inflight++
		go func() {
			ans := upstreamAnswer{}
			defer func() { replies <- ans }()
			answer, _, err := upstreamClient.Exchange(r.Copy(), up.addr)
			if err == nil {
				ans = upstreamAnswer{resp: answer, addr: up.addr}
			}
		}()
	}
	if len(upstreams) > 0 {
		launch()
	}
	hedge := time.NewTimer(hedgeDelay)
	defer hedge.Stop()
	for inflight > 0 {
		select {
		case resp := <-replies:
			inflight--
			if resp.resp != nil {
				return resp
			}
			// That upstream failed outright: move on now rather than
			// waiting out the hedge timer.
			if next < len(upstreams) {
				launch()
				hedge.Reset(hedgeDelay)
			}
		case <-hedge.C:
			if next < len(upstreams) {
				launch()
				hedge.Reset(hedgeDelay)
			}
		}
	}
	return upstreamAnswer{}
}

// Owns reports whether the server answers name itself from this snapshot
// rather than forwarding it — the same lookup the query path makes.
func (z *ZoneSnapshot) Owns(name string) bool {
	_, ok := lookupA(z, strings.ToLower(dnsName(name)))
	return ok
}

// ResolveA resolves name the way the server resolves a name it does not own:
// through this snapshot's upstreams (the defaults when it declares none),
// bypassing its own records. Returns the first A record.
func (z *ZoneSnapshot) ResolveA(name string) (net.IP, error) {
	upstreams := z.upstream
	if len(upstreams) == 0 {
		upstreams = defaultUpstream()
	}
	var q miekg.Msg
	q.SetQuestion(dnsName(name), miekg.TypeA)
	q.RecursionDesired = true
	ans := exchangeUpstream(&q, upstreams)
	if ans.resp == nil {
		return nil, fmt.Errorf("no upstream DNS server answered for %s", name)
	}
	for _, rr := range ans.resp.Answer {
		if a, ok := rr.(*miekg.A); ok {
			return a.A, nil
		}
	}
	return nil, fmt.Errorf("no A record for %s upstream (%s)", name, miekg.RcodeToString[ans.resp.Rcode])
}

// IsBindPermissionErr reports whether err is the classic "cannot bind port
// < 1024 as an unprivileged user" failure, so callers can print the exact fix
// instead of a raw syscall error.
func IsBindPermissionErr(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}
