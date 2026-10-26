# Metrics

An opt-in Prometheus and Grafana stack that scrapes Traefik's `/metrics`
endpoint:

```sh
srv metrics enable
#   https://grafana.local      (admin / admin)
#   https://prometheus.local
srv metrics status
srv metrics disable
```

Both UIs are routed through Traefik with local TLS; no loopback ports are
exposed. Grafana comes with a Prometheus datasource already wired; import
dashboard ID 17347 for a per-router Traefik overview.
