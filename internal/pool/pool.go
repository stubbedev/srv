// Package pool manages shared PHP-FPM containers. Multiple PHP sites whose
// (php_version, sorted_ext_set) fingerprint matches collapse into one FPM
// container, mounting each project at /var/www/<sitename>. nginx web
// containers in each site point fastcgi_pass at the pool container by name.
//
// Per-site state is still owned by internal/site; this package is the
// authoritative writer for the pool's compose file under
// ~/.config/srv/fpm/<fingerprint>/.
package pool

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// Member describes one PHP site participating in a pool.
type Member struct {
	SiteName    string
	ProjectPath string
}

// Spec is the input to WriteAndUp. PHPVersion + Extensions define the
// fingerprint; Members lists every site sharing this pool.
type Spec struct {
	PHPVersion string
	Extensions []string // already-sanitised, non-builtin
	Members    []Member
}

// Fingerprint returns the stable 12-char tag derived from (PHPVersion,
// sorted extensions). Mirrors site.PHPImageFingerprint without importing it
// (avoids an import cycle: site → pool → site).
func Fingerprint(phpVersion string, extensions []string) string {
	exts := append([]string(nil), extensions...)
	sort.Strings(exts)
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|%s", phpVersion, strings.Join(exts, ","))
	return fmt.Sprintf("%x", h.Sum(nil)[:6])
}

// ContainerName returns the docker container name for the FPM container.
func ContainerName(fingerprint string) string {
	return "srv-fpm-" + fingerprint
}

// ImageTag returns the docker image tag the pool container runs.
func ImageTag(fingerprint string) string {
	return "srv-php:" + fingerprint
}

// Dir returns the on-disk directory housing the pool's docker-compose.yml,
// Dockerfile, php.ini, and php-fpm.conf.
func Dir(cfg *config.Config, fingerprint string) string {
	return filepath.Join(cfg.Root, "fpm", fingerprint)
}

// SiteMountPath returns the path inside the FPM container at which the
// site's project is bind-mounted. Used by both the pool compose and the
// per-site nginx config so that fastcgi SCRIPT_FILENAME resolves on both
// ends.
func SiteMountPath(siteName string) string {
	return constants.PHPSiteDocRootRoot + "/" + siteName
}

// composeFile mirrors the subset of docker-compose schema we emit.
type composeFile struct {
	Name     string                       `yaml:"name,omitempty"`
	Services map[string]composeService    `yaml:"services"`
	Networks map[string]composeNetworkRef `yaml:"networks"`
}

type composeService struct {
	Build         *composeBuild        `yaml:"build,omitempty"`
	Image         string               `yaml:"image,omitempty"`
	PullPolicy    string               `yaml:"pull_policy,omitempty"`
	ContainerName string               `yaml:"container_name"`
	Volumes       []composeVolume      `yaml:"volumes,omitempty"`
	Networks      []string             `yaml:"networks"`
	Restart       string               `yaml:"restart"`
	HealthCheck   *composeHealthCheck  `yaml:"healthcheck,omitempty"`
	Labels        map[string]string    `yaml:"labels,omitempty"`
}

type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

type composeVolume struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only,omitempty"`
}

type composeHealthCheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
}

type composeNetworkRef struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

// WriteFiles renders Dockerfile + docker-compose.yml + php.ini + php-fpm.conf
// into the pool's directory using the supplied spec. The caller is responsible
// for then running `docker compose up -d` in the pool directory.
//
// dockerfile, phpIni, fpmConf are the rendered contents the caller wants
// shared across every member of this pool — kept as parameters so this
// package does not depend on internal/site.
func WriteFiles(cfg *config.Config, spec Spec, dockerfile, phpIni, fpmConf string) error {
	fp := Fingerprint(spec.PHPVersion, spec.Extensions)
	dir := Dir(cfg, fp)
	if err := os.MkdirAll(dir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("create pool dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, constants.PHPDockerfileFile), []byte(dockerfile), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, constants.PHPIniFile), []byte(phpIni), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write php.ini: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, constants.PHPFPMConfFile), []byte(fpmConf), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write php-fpm.conf: %w", err)
	}

	compose := buildCompose(cfg, fp, dir, spec.Members)
	data, err := yaml.Marshal(&compose)
	if err != nil {
		return fmt.Errorf("marshal compose: %w", err)
	}
	header := fmt.Sprintf("# Generated by srv — shared PHP-FPM pool (fingerprint %s)\n# Members: %d\n\n", fp, len(spec.Members))
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), append([]byte(header), data...), constants.FilePermDefault); err != nil {
		return fmt.Errorf("write compose: %w", err)
	}
	return nil
}

func buildCompose(cfg *config.Config, fp, dir string, members []Member) composeFile {
	volumes := []composeVolume{
		{Type: "bind", Source: filepath.Join(dir, constants.PHPIniFile), Target: constants.PHPIniContainerPath, ReadOnly: true},
		{Type: "bind", Source: filepath.Join(dir, constants.PHPFPMConfFile), Target: constants.PHPFPMConfContainerPath, ReadOnly: true},
	}
	// Deterministic ordering so the compose file is stable across edits.
	sorted := append([]Member(nil), members...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SiteName < sorted[j].SiteName })
	for _, m := range sorted {
		volumes = append(volumes, composeVolume{
			Type:   "bind",
			Source: m.ProjectPath,
			Target: SiteMountPath(m.SiteName),
		})
	}

	return composeFile{
		Name: constants.ComposeProjectName,
		Services: map[string]composeService{
			"fpm": {
				Build: &composeBuild{
					Context:    dir,
					Dockerfile: constants.PHPDockerfileFile,
				},
				Image:         ImageTag(fp),
				PullPolicy:    "missing",
				ContainerName: ContainerName(fp),
				Volumes:       volumes,
				Networks:      []string{constants.TraefikSubdir},
				Restart:       constants.RestartUnlessStopped,
				HealthCheck: &composeHealthCheck{
					Test:        []string{"CMD-SHELL", fmt.Sprintf("nc -z 127.0.0.1 %d || exit 1", constants.PHPFPMPort)},
					Interval:    "30s",
					Timeout:     "3s",
					StartPeriod: "5s",
					Retries:     3,
				},
				Labels: map[string]string{
					constants.LabelSrvSite: "fpm-pool-" + fp,
					constants.LabelSrvType: "php-fpm-pool",
				},
			},
		},
		Networks: map[string]composeNetworkRef{
			constants.TraefikSubdir: {Name: cfg.NetworkName, External: true},
		},
	}
}

// Remove tears down the pool's compose project and deletes its directory.
// Safe to call when the directory does not exist.
func Remove(cfg *config.Config, fingerprint string) error {
	dir := Dir(cfg, fingerprint)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(dir)
}
