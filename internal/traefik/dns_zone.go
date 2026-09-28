package traefik

import (
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
	"github.com/stubbedev/srv/internal/ops"
)

// PinLocalDomains adds the local-domain registry to z: every entry answers
// with loopback, a wildcard entry covering its apex and every subdomain. It is
// the one translation from registry entries to DNS answers, shared by the
// daemon's live zones and by callers asking whether srv shadows a name.
func PinLocalDomains(z *dnsd.ZoneSnapshot) error {
	domains, err := LoadLocalDomains()
	if err != nil {
		return err
	}
	for _, entry := range domains {
		if IsWildcardEntry(entry) {
			z.PinWildcard(BareDomain(entry), constants.LocalhostIP)
			continue
		}
		z.PinExact(entry, constants.LocalhostIP)
	}
	return nil
}

// SetConfiguredUpstreams adds config.yml's upstream_dns servers to z. With
// none configured z keeps no upstreams and dnsd applies its defaults.
func SetConfiguredUpstreams(z *dnsd.ZoneSnapshot) error {
	userCfg, err := ops.UserConfig()
	if err != nil {
		return err
	}
	for _, spec := range userCfg.UpstreamDNS {
		z.SetUpstream(spec)
	}
	return nil
}
