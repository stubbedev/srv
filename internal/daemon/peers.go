package daemon

import (
	"fmt"
	"time"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/ops"
	"github.com/stubbedev/srv/internal/peer"
)

const peerRestartDelay = 5 * time.Second

func (d *Daemon) startPeerDiscovery() {
	userCfg, err := ops.UserConfig()
	if err != nil {
		d.log("Peer discovery off: %v", err)
		return
	}
	if userCfg.PeerName == "" {
		return
	}
	go func() {
		for {
			err := d.runPeerNode(userCfg.PeerName)
			if d.ctx.Err() != nil {
				return
			}
			d.log("Peer discovery stopped (%v); restarting in %s", err, peerRestartDelay)
			select {
			case <-d.ctx.Done():
				return
			case <-time.After(peerRestartDelay):
			}
		}
	}()
}

func (d *Daemon) runPeerNode(name peer.Name) error {
	node, err := peer.Listen(name, constants.PortPeer, d.cfg.PeersPath(), func(entries []peer.Entry) {
		d.peers.Store(&entries)
		d.refreshDNSZones()
	})
	if err != nil {
		return err
	}
	d.log("Peer discovery announcing %s on udp port %d", name.FQDN(), constants.PortPeer)
	if err := node.Run(d.ctx); err != nil {
		return fmt.Errorf("peer discovery as %s: %w", name, err)
	}
	return nil
}
