# LAN peers

Machines running srv on the same LAN can reach each other by name, as
`<name>.home.arpa`. `home.arpa` is the domain RFC 8375 reserves for home
networks, so these names never collide with real DNS or with mDNS
`.local` names.

## Turning it on

Give each machine a name in `~/.config/srv/config.yml`:

```yaml
peer_name: laptop
```

Restart the daemon (`srv daemon restart`) and re-run `srv install` once,
so the system resolver routes `home.arpa` to srv's DNS. From then on,
every other peer resolves:

```sh
ssh laptop.home.arpa
srv peer list
```

Names are lowercase letters, digits and hyphens, up to 63 characters.

## How it works

Every peer broadcasts a small UDP announce on port 15354 to each of its
IPv4 networks when it starts and every 30 seconds. A peer that hears an
announce records the sender's name and source address, and answers it
directly over unicast with its own announce, so a newcomer learns every
peer within a round trip. A peer that has not been heard from for 90
seconds is dropped.

srv uses broadcast rather than mDNS multicast. Wi-Fi access points that
snoop IGMP forward multicast only to clients they saw join the group, and
a client that roams to another access point keeps its address and never
joins again, so mDNS goes quiet after a roam. Broadcast reaches every
client, and the answers are unicast, which reaches a phone even while
Android filters incoming multicast and broadcast.

The peer table lives in `~/.config/srv/peers.json`. The daemon serves it
through srv's DNS; `srv peer list` and `srv peer dial` read it directly.

Anyone on the LAN can claim a name. Use it to find a machine, and let the
protocol on top (ssh host keys, TLS) authenticate it.

## Firewall

Allow inbound UDP on port 15354 on every peer. On NixOS:

```nix
networking.firewall.allowedUDPPorts = [ 15354 ];
```

## Machines without the daemon

A machine with no container runtime or service manager, such as a phone
running Termux, runs discovery on its own:

```sh
srv peer run
```

Android hides the network interface list from apps, so there srv cannot
compute each network's broadcast address and announces on the limited
broadcast address 255.255.255.255, which leaves through the active
network.

If its resolver cannot be pointed at srv's DNS, connect through the peer
table instead, for example as an ssh `ProxyCommand`:

```sshconfig
Host laptop
  ProxyCommand srv peer dial %h %p
```
