package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

type Node struct {
	name      Name
	conn      *net.UDPConn
	tablePath string
	onChange  func([]Entry)
	now       func() time.Time
	targets   func() ([]netip.AddrPort, error)
}

type packet struct {
	data []byte
	from netip.AddrPort
}

func Listen(name Name, port uint16, tablePath string, onChange func([]Entry)) (*Node, error) {
	lc := net.ListenConfig{Control: func(_, _ string, rc syscall.RawConn) error {
		var sockErr error
		if err := rc.Control(func(fd uintptr) { sockErr = enableBroadcast(fd) }); err != nil {
			return err
		}
		return sockErr
	}}
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(int(port)))
	pc, err := lc.ListenPacket(context.Background(), "udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for peers on udp %s: %w", addr, err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("listen for peers on udp %s: got %T, want *net.UDPConn", addr, pc)
	}
	return &Node{
		name:      name,
		conn:      conn,
		tablePath: tablePath,
		onChange:  onChange,
		now:       func() time.Time { return time.Now().UTC() },
		targets:   func() ([]netip.AddrPort, error) { return broadcastTargets(port) },
	}, nil
}

func (n *Node) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = n.conn.Close() }()

	entries, err := Load(n.tablePath, n.now())
	if err != nil {
		return err
	}
	t := table{}
	for _, e := range entries {
		t.observe(e)
	}
	n.onChange(t.sorted())

	packets := make(chan packet)
	readErr := make(chan error, 1)
	go n.read(ctx, packets, readErr)

	if err := n.announce(); err != nil {
		return err
	}
	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			return err
		case p := <-packets:
			if err := n.handle(t, p); err != nil {
				return err
			}
		case <-ticker.C:
			if t.expire(n.now()) {
				if err := n.publish(t); err != nil {
					return err
				}
			}
			if err := n.announce(); err != nil {
				return err
			}
		}
	}
}

func (n *Node) read(ctx context.Context, packets chan<- packet, readErr chan<- error) {
	stop := context.AfterFunc(ctx, func() { _ = n.conn.Close() })
	defer stop()
	buf := make([]byte, maxMessageSize)
	for {
		size, from, err := n.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				readErr <- fmt.Errorf("read peer announce: %w", err)
			}
			return
		}
		select {
		case packets <- packet{data: append([]byte(nil), buf[:size]...), from: from}:
		case <-ctx.Done():
			return
		}
	}
}

func (n *Node) handle(t table, p packet) error {
	m, err := decode(p.data)
	if err != nil || m.Name == n.name {
		return nil //nolint:nilerr // a malformed datagram from the LAN is dropped, not fatal
	}
	addrChanged := t.observe(Entry{Name: m.Name, Addr: p.from.Addr().Unmap(), Seen: n.now()})
	if err := save(n.tablePath, t.sorted()); err != nil {
		return err
	}
	if addrChanged {
		n.onChange(t.sorted())
	}
	if m.Reply {
		return nil
	}
	return n.send(message{Name: n.name, Reply: true}, p.from)
}

func (n *Node) publish(t table) error {
	entries := t.sorted()
	if err := save(n.tablePath, entries); err != nil {
		return err
	}
	n.onChange(entries)
	return nil
}

func (n *Node) announce() error {
	targets, err := n.targets()
	if err != nil {
		return err
	}
	var errs []error
	for _, to := range targets {
		if err := n.send(message{Name: n.name}, to); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (n *Node) send(m message, to netip.AddrPort) error {
	data, err := encode(m)
	if err != nil {
		return err
	}
	if _, err := n.conn.WriteToUDPAddrPort(data, to); err != nil {
		return fmt.Errorf("send peer announce to %s: %w", to, err)
	}
	return nil
}
