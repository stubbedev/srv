package peer

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
)

func broadcastTargets(port uint16) ([]netip.AddrPort, error) {
	addrs, err := interfaceBroadcasts()
	if err != nil {
		return nil, err
	}
	targets := make([]netip.AddrPort, len(addrs))
	for i, a := range addrs {
		targets[i] = netip.AddrPortFrom(a, port)
	}
	return targets, nil
}

func interfaceBroadcasts() ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	var out []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 ||
			iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses of %s: %w", iface.Name, err)
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if b, ok := broadcastOf(ipnet); ok {
				out = append(out, b)
			}
		}
	}
	slices.SortFunc(out, netip.Addr.Compare)
	return slices.Compact(out), nil
}

func broadcastOf(ipnet *net.IPNet) (netip.Addr, bool) {
	ip4 := ipnet.IP.To4()
	if ip4 == nil {
		return netip.Addr{}, false
	}
	ones, bits := ipnet.Mask.Size()
	if bits == 0 || bits-ones < 2 {
		return netip.Addr{}, false
	}
	mask := ipnet.Mask[len(ipnet.Mask)-net.IPv4len:]
	var b [net.IPv4len]byte
	for i := range b {
		b[i] = ip4[i] | ^mask[i]
	}
	return netip.AddrFrom4(b), true
}
