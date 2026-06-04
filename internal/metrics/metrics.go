// Package metrics owns the opt-in prometheus + grafana stack used by
// `srv metrics`. Both services run as containers on the shared srv network so
// prometheus can scrape Traefik's existing /metrics endpoint directly by
// container name.
package metrics

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/platform"
)

// isLinux is swapped by tests to exercise both Linux and Docker-Desktop
// template branches without depending on runtime.GOOS.
var isLinux = platform.IsLinux

const (
	// PrometheusImage is the prometheus image used by the metrics stack.
	PrometheusImage = "prom/prometheus:latest"
	// GrafanaImage is the grafana image used by the metrics stack.
	GrafanaImage = "grafana/grafana:latest"
	// PrometheusContainer is the container name for prometheus.
	PrometheusContainer = "srv-prometheus"
	// GrafanaContainer is the container name for grafana.
	GrafanaContainer = "srv-grafana"
	// GrafanaDomain is the local HTTPS hostname for the Grafana UI.
	GrafanaDomain = "grafana.local"
	// PrometheusDomain is the local HTTPS hostname for the Prometheus UI.
	PrometheusDomain = "prometheus.local"
	// ProxySiteName groups the metrics certs under one synthetic site dir
	// (~/.config/srv/sites/_proxy-metrics/certs/).
	ProxySiteName = "_proxy-metrics"
	// GrafanaHostPort is the loopback port the Grafana container binds on
	// Linux (host networking) so host-network Traefik can reach it. A
	// non-standard port avoids clashing with dev servers on :3000.
	GrafanaHostPort = 13000
	// PrometheusHostPort is the loopback port the Prometheus container binds
	// on Linux (host networking). Non-standard to avoid clashing with :9090.
	PrometheusHostPort = 19090
)

// Dir returns the on-disk directory for the metrics stack.
func Dir(cfg *config.Config) string {
	return filepath.Join(cfg.Root, "metrics")
}

// ComposePath returns the metrics stack's docker-compose.yml path.
func ComposePath(cfg *config.Config) string {
	return filepath.Join(Dir(cfg), "docker-compose.yml")
}

// IsConfigured reports whether the metrics stack has been set up — i.e.
// `srv metrics enable` ran at some point and left its compose file on disk.
// Used by `srv install` to bring a previously-enabled stack back up (it does
// not survive a reboot otherwise) and by `srv doctor` to detect routes that
// point at a stopped stack.
func IsConfigured(cfg *config.Config) bool {
	_, err := os.Stat(ComposePath(cfg))
	return err == nil
}

// TraefikConfigPath returns the Traefik file-provider yaml that routes
// grafana.local + prometheus.local at the stack's two containers.
func TraefikConfigPath(cfg *config.Config) string {
	return filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+"metrics"+constants.ExtYAML)
}

// =============================================================================
// Typed config models. Each generated file is built from structs and
// marshalled once (see marshalYAML) rather than assembled by string
// interpolation, so every value is YAML-encoded as a scalar and cannot break
// the document. A leading comment header is prepended after marshalling because
// yaml.Marshal does not emit comments.
// =============================================================================

// emptyMap marshals as `{}` — used for Traefik's `tls: {}` and compose named
// volumes (`prometheus-data: {}`).
type emptyMap struct{}

// Traefik file-provider dynamic config (http routers + services).
type tfRouter struct {
	Rule        string   `yaml:"rule"`
	EntryPoints []string `yaml:"entryPoints"`
	Service     string   `yaml:"service"`
	TLS         emptyMap `yaml:"tls"`
}

type tfServer struct {
	URL string `yaml:"url"`
}

type tfService struct {
	LoadBalancer struct {
		Servers []tfServer `yaml:"servers"`
	} `yaml:"loadBalancer"`
}

type tfDynamic struct {
	HTTP struct {
		Routers  map[string]tfRouter  `yaml:"routers"`
		Services map[string]tfService `yaml:"services"`
	} `yaml:"http"`
}

func tfServiceFor(url string) tfService {
	var s tfService
	s.LoadBalancer.Servers = []tfServer{{URL: url}}
	return s
}

// Prometheus scrape config.
type promConfig struct {
	Global struct {
		ScrapeInterval     string `yaml:"scrape_interval"`
		EvaluationInterval string `yaml:"evaluation_interval"`
	} `yaml:"global"`
	ScrapeConfigs []promScrapeConfig `yaml:"scrape_configs"`
}

type promScrapeConfig struct {
	JobName       string `yaml:"job_name"`
	StaticConfigs []struct {
		Targets []string `yaml:"targets"`
	} `yaml:"static_configs"`
}

// Grafana datasource provisioning.
type grafanaProvisioning struct {
	APIVersion  int                 `yaml:"apiVersion"`
	Datasources []grafanaDatasource `yaml:"datasources"`
}

type grafanaDatasource struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	Access    string `yaml:"access"`
	URL       string `yaml:"url"`
	IsDefault bool   `yaml:"isDefault"`
	Editable  bool   `yaml:"editable"`
}

// docker-compose model for the metrics stack.
type composeService struct {
	Image         string   `yaml:"image"`
	ContainerName string   `yaml:"container_name"`
	Restart       string   `yaml:"restart"`
	NetworkMode   string   `yaml:"network_mode,omitempty"`
	Command       []string `yaml:"command,omitempty"`
	Environment   []string `yaml:"environment,omitempty"`
	Volumes       []string `yaml:"volumes,omitempty"`
	Labels        []string `yaml:"labels,omitempty"`
	DependsOn     []string `yaml:"depends_on,omitempty"`
	Networks      []string `yaml:"networks,omitempty"`
}

type composeNetwork struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

type composeFile struct {
	Name     string                     `yaml:"name"`
	Services map[string]*composeService `yaml:"services"`
	Volumes  map[string]emptyMap        `yaml:"volumes,omitempty"`
	Networks map[string]composeNetwork  `yaml:"networks,omitempty"`
}

// marshalYAML renders v as YAML with the given comment header prepended. The
// error is ignored: these models are fixed-shape structs of scalars and slices,
// which yaml.Marshal cannot fail to encode.
func marshalYAML(header string, v any) string {
	data, _ := yaml.Marshal(v) //nolint:errcheck // static structs never fail to marshal
	return header + string(data)
}

// WriteTraefikConfig emits the file-provider yaml so Traefik routes
// https://grafana.local and https://prometheus.local at the two containers.
// Both routers use TLS with mkcert-issued certs.
//
// On Linux, Traefik runs in host-network mode and cannot resolve the metrics
// container names, so it reaches them via the loopback host ports the compose
// stack publishes. On Mac/Windows, Traefik shares the srv network and routes
// to the containers by name.
func WriteTraefikConfig(cfg *config.Config) error {
	grafanaTarget := fmt.Sprintf("http://%s:3000", GrafanaContainer)
	prometheusTarget := fmt.Sprintf("http://%s:9090", PrometheusContainer)
	if isLinux() {
		grafanaTarget = fmt.Sprintf("http://127.0.0.1:%d", GrafanaHostPort)
		prometheusTarget = fmt.Sprintf("http://127.0.0.1:%d", PrometheusHostPort)
	}

	var doc tfDynamic
	doc.HTTP.Routers = map[string]tfRouter{
		"metrics-grafana": {
			Rule:        fmt.Sprintf("Host(`%s`)", GrafanaDomain),
			EntryPoints: []string{"websecure"},
			Service:     "metrics-grafana",
		},
		"metrics-prometheus": {
			Rule:        fmt.Sprintf("Host(`%s`)", PrometheusDomain),
			EntryPoints: []string{"websecure"},
			Service:     "metrics-prometheus",
		},
	}
	doc.HTTP.Services = map[string]tfService{
		"metrics-grafana":    tfServiceFor(grafanaTarget),
		"metrics-prometheus": tfServiceFor(prometheusTarget),
	}

	body := marshalYAML("# Generated by srv — metrics HTTPS routers\n", doc)
	return os.WriteFile(TraefikConfigPath(cfg), []byte(body), constants.FilePermDefault)
}

// RemoveTraefikConfig deletes the file-provider yaml. Idempotent.
func RemoveTraefikConfig(cfg *config.Config) error {
	if err := os.Remove(TraefikConfigPath(cfg)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WriteStack renders the docker-compose.yml + prometheus.yml + grafana
// provisioning files into the metrics directory. Idempotent.
func WriteStack(cfg *config.Config) error {
	dir := Dir(cfg)
	provDir := filepath.Join(dir, "grafana-provisioning", "datasources")
	if err := os.MkdirAll(provDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "prometheus.yml"), []byte(prometheusYAML()), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write prometheus.yml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(provDir, "prometheus.yml"), []byte(grafanaDatasourceYAML()), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write grafana datasource: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeYAML(cfg.NetworkName)), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write compose: %w", err)
	}
	return nil
}

func prometheusYAML() string {
	// Linux: prometheus runs in host-network mode (like Traefik) and reaches
	// Traefik's metrics endpoint on the loopback. Mac/Windows: prometheus is
	// on the srv network and reaches Traefik by container name.
	traefikTarget := "srv-traefik:8080"
	if isLinux() {
		traefikTarget = "127.0.0.1:8080"
	}

	var doc promConfig
	doc.Global.ScrapeInterval = "15s"
	doc.Global.EvaluationInterval = "15s"
	scrape := promScrapeConfig{JobName: "traefik"}
	scrape.StaticConfigs = []struct {
		Targets []string `yaml:"targets"`
	}{{Targets: []string{traefikTarget}}}
	doc.ScrapeConfigs = []promScrapeConfig{scrape}

	return marshalYAML("# Generated by srv — prometheus scrape config\n", doc)
}

func grafanaDatasourceYAML() string {
	// Linux: both services are host-networked, so grafana reaches prometheus
	// on the loopback. Mac/Windows: by container name on the srv network.
	promURL := "http://srv-prometheus:9090"
	if isLinux() {
		promURL = fmt.Sprintf("http://127.0.0.1:%d", PrometheusHostPort)
	}

	doc := grafanaProvisioning{
		APIVersion: 1,
		Datasources: []grafanaDatasource{{
			Name:      "Prometheus",
			Type:      "prometheus",
			Access:    "proxy",
			URL:       promURL,
			IsDefault: true,
			Editable:  true,
		}},
	}
	return marshalYAML("# Generated by srv — pre-wired Prometheus datasource\n", doc)
}

// composeYAML renders the metrics stack's docker-compose.yml. The Linux and
// Docker-Desktop shapes differ enough to warrant separate builders.
func composeYAML(networkName string) string {
	if isLinux() {
		return composeYAMLHost()
	}
	return composeYAMLBridge(networkName)
}

// composeYAMLHost renders the Linux stack. Both services use host networking —
// the same model srv uses for Traefik — so prometheus can scrape Traefik on
// 127.0.0.1:8080 and Traefik can route to the UIs on the loopback. Each UI is
// pinned to a loopback bind address + non-standard port so it is reachable by
// host-network Traefik without being exposed off-host or clashing with the
// usual :3000 / :9090 dev-server ports.
func composeYAMLHost() string {
	doc := composeFile{
		Name: constants.MetricsComposeProject,
		Services: map[string]*composeService{
			"prometheus": {
				Image:         PrometheusImage,
				ContainerName: PrometheusContainer,
				Restart:       "unless-stopped",
				NetworkMode:   "host",
				Command: []string{
					"--config.file=/etc/prometheus/prometheus.yml",
					"--storage.tsdb.path=/prometheus",
					fmt.Sprintf("--web.listen-address=127.0.0.1:%d", PrometheusHostPort),
				},
				Volumes: []string{
					"./prometheus.yml:/etc/prometheus/prometheus.yml:ro",
					"prometheus-data:/prometheus",
				},
				Labels: []string{
					constants.LabelSrvSite + "=" + PrometheusDomain,
					constants.LabelSrvType + "=prometheus",
				},
			},
			"grafana": {
				Image:         GrafanaImage,
				ContainerName: GrafanaContainer,
				Restart:       "unless-stopped",
				NetworkMode:   "host",
				Environment: []string{
					"GF_SECURITY_ADMIN_USER=admin",
					"GF_SECURITY_ADMIN_PASSWORD=admin",
					"GF_AUTH_ANONYMOUS_ENABLED=true",
					"GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer",
					"GF_SERVER_ROOT_URL=https://" + GrafanaDomain,
					"GF_SERVER_HTTP_ADDR=127.0.0.1",
					fmt.Sprintf("GF_SERVER_HTTP_PORT=%d", GrafanaHostPort),
				},
				Volumes: []string{
					"grafana-data:/var/lib/grafana",
					"./grafana-provisioning:/etc/grafana/provisioning:ro",
				},
				Labels: []string{
					constants.LabelSrvSite + "=" + GrafanaDomain,
					constants.LabelSrvType + "=grafana",
				},
				DependsOn: []string{"prometheus"},
			},
		},
		Volumes: map[string]emptyMap{"prometheus-data": {}, "grafana-data": {}},
	}
	return marshalYAML("# Generated by srv — metrics stack (prometheus + grafana)\n# Linux: host networking, matching how srv runs Traefik.\n", doc)
}

// composeYAMLBridge renders the Docker-Desktop (Mac/Windows) stack. There
// Traefik shares the srv bridge network, so the services join it and are
// reached by container name — no host networking, no published ports.
func composeYAMLBridge(networkName string) string {
	doc := composeFile{
		Name: constants.MetricsComposeProject,
		Services: map[string]*composeService{
			"prometheus": {
				Image:         PrometheusImage,
				ContainerName: PrometheusContainer,
				Restart:       "unless-stopped",
				Volumes: []string{
					"./prometheus.yml:/etc/prometheus/prometheus.yml:ro",
					"prometheus-data:/prometheus",
				},
				Labels: []string{
					constants.LabelSrvSite + "=" + PrometheusDomain,
					constants.LabelSrvType + "=prometheus",
				},
				Networks: []string{"traefik"},
			},
			"grafana": {
				Image:         GrafanaImage,
				ContainerName: GrafanaContainer,
				Restart:       "unless-stopped",
				Environment: []string{
					"GF_SECURITY_ADMIN_USER=admin",
					"GF_SECURITY_ADMIN_PASSWORD=admin",
					"GF_AUTH_ANONYMOUS_ENABLED=true",
					"GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer",
					"GF_SERVER_ROOT_URL=https://" + GrafanaDomain,
				},
				Volumes: []string{
					"grafana-data:/var/lib/grafana",
					"./grafana-provisioning:/etc/grafana/provisioning:ro",
				},
				Labels: []string{
					constants.LabelSrvSite + "=" + GrafanaDomain,
					constants.LabelSrvType + "=grafana",
				},
				DependsOn: []string{"prometheus"},
				Networks:  []string{"traefik"},
			},
		},
		Volumes:  map[string]emptyMap{"prometheus-data": {}, "grafana-data": {}},
		Networks: map[string]composeNetwork{"traefik": {Name: networkName, External: true}},
	}
	return marshalYAML("# Generated by srv — metrics stack (prometheus + grafana)\n", doc)
}
