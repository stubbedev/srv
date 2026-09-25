// miniclient.go is a minimal Docker Engine API client over HTTP, speaking to
// the unix socket or TCP endpoint the engine resolver exports as
// DOCKER_HOST. It implements exactly the nine calls srv uses (ping, network
// CRUD + connect, container inspect/list, image pull, event stream) — the
// full SDK pulled in forty packages and a telemetry chain for this surface.
// Paths are unversioned: the daemon serves them at its current API version,
// which replaces the SDK's version negotiation.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/stubbedev/srv/internal/ops"
)

// conflictError marks the HTTP 409 the daemon returns for "already exists"
// (network create, network connect). IsConflict is the portable check — no
// error-text matching.
type conflictError struct{ op string }

func (e *conflictError) Error() string { return e.op + ": conflict (already exists?)" }

// IsConflict reports whether err is the daemon's 409 "already exists" class.
func IsConflict(err error) bool {
	var c *conflictError
	return errors.As(err, &c)
}

// networkSummary is the subset of GET /networks srv reads.
type networkSummary struct {
	Name string `json:"Name"`
}

// containerSummary is the subset of GET /containers/json srv reads.
type containerSummary struct {
	State string `json:"State"`
}

// inspectState / inspectConfig are the nested subsets of GET
// /containers/{id}/json srv reads.
type inspectState struct {
	Running bool `json:"Running"`
}

type inspectConfig struct {
	Image string `json:"Image"`
}

// inspectResponse is the subset of GET /containers/{id}/json srv reads.
type inspectResponse struct {
	State  *inspectState  `json:"State"`
	Config *inspectConfig `json:"Config"`
}

// EventActor is the event's actor: named attributes, of which srv reads
// "name".
type EventActor struct {
	Attributes map[string]string `json:"Attributes"`
}

// Event is the subset of a container event stream message srv reads.
type Event struct {
	Actor EventActor `json:"Actor"`
}

// miniClient talks to the daemon endpoint over plain HTTP.
type miniClient struct {
	client *http.Client
	// host is the authority requests are addressed to. For a unix socket it
	// is a placeholder the dialer ignores; for tcp it is the real host:port.
	host string
}

// newMiniClient dials the daemon the engine resolver selected. Resolving the
// engine exports DOCKER_HOST, which is what the endpoint below reads — so
// this call is what points the client at Podman rather than Docker.
func newMiniClient() (*miniClient, error) {
	_ = ops.Engine()
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}

	transport := &http.Transport{}
	authority := "d"
	switch {
	case strings.HasPrefix(host, "unix://"):
		socket := strings.TrimPrefix(host, "unix://")
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
	case strings.HasPrefix(host, "tcp://"):
		u, err := url.Parse(host)
		if err != nil {
			return nil, fmt.Errorf("invalid DOCKER_HOST %q: %w", host, err)
		}
		authority = u.Host
		transport.Proxy = http.ProxyFromEnvironment
	default:
		return nil, fmt.Errorf("unsupported DOCKER_HOST %q (only unix:// and tcp:// endpoints are supported)", host)
	}
	return &miniClient{client: &http.Client{Transport: transport}, host: authority}, nil
}

// encodeFilters marshals the Docker filter query format: {"name":["x"]}.
func encodeFilters(filters map[string][]string) string {
	encoded, err := json.Marshal(filters)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// Ping checks daemon availability.
func (m *miniClient) Ping(ctx context.Context) error {
	return m.do(ctx, http.MethodGet, "/_ping", nil, nil, nil)
}

// NetworkList returns networks matching the name filter (the daemon matches
// by prefix; callers exact-match on Name themselves).
func (m *miniClient) NetworkList(ctx context.Context, nameFilter string) ([]networkSummary, error) {
	var networks []networkSummary
	q := url.Values{"filters": []string{encodeFilters(map[string][]string{"name": {nameFilter}})}}
	if err := m.do(ctx, http.MethodGet, "/networks", q, nil, &networks); err != nil {
		return nil, err
	}
	return networks, nil
}

// NetworkCreate creates a bridge network; a 409 ("already exists") surfaces
// as *conflictError for callers to treat as success.
func (m *miniClient) NetworkCreate(ctx context.Context, name, driver string) error {
	return m.do(ctx, http.MethodPost, "/networks", nil, map[string]string{"Name": name, "Driver": driver}, nil)
}

// NetworkRemove deletes a network by name.
func (m *miniClient) NetworkRemove(ctx context.Context, name string) error {
	return m.do(ctx, http.MethodDelete, "/networks/"+name, nil, nil, nil)
}

// NetworkConnect attaches a container (by ID or name) to a network, with
// optional aliases.
func (m *miniClient) NetworkConnect(ctx context.Context, networkName, containerID string, aliases []string) error {
	body := map[string]any{"Container": containerID}
	if len(aliases) > 0 {
		body["EndpointConfig"] = map[string]any{"Aliases": aliases}
	}
	return m.do(ctx, http.MethodPost, "/networks/"+networkName+"/connect", nil, body, nil)
}

// ContainerInspect fetches the inspect subset for a container (by name or ID).
func (m *miniClient) ContainerInspect(ctx context.Context, name string) (inspectResponse, error) {
	var info inspectResponse
	if err := m.do(ctx, http.MethodGet, "/containers/"+name+"/json", nil, nil, &info); err != nil {
		return inspectResponse{}, err
	}
	return info, nil
}

// ContainerList lists containers (all states when all), filtered by one
// label key=value pair when given.
func (m *miniClient) ContainerList(ctx context.Context, all bool, labelFilter string) ([]containerSummary, error) {
	q := url.Values{}
	if all {
		q.Set("all", "1")
	}
	if labelFilter != "" {
		q.Set("filters", encodeFilters(map[string][]string{"label": {labelFilter}}))
	}
	var containers []containerSummary
	if err := m.do(ctx, http.MethodGet, "/containers/json", q, nil, &containers); err != nil {
		return nil, err
	}
	return containers, nil
}

// ImagePull starts pulling ref and returns the JSON progress stream. The
// caller must consume and close it to drive the transfer.
func (m *miniClient) ImagePull(ctx context.Context, ref string) (io.ReadCloser, error) {
	// ref is "name[:tag]" (srv never pulls by digest); the API wants the tag
	// split out into its own parameter.
	imageName, tag := splitImageRef(ref)
	q := url.Values{"fromImage": []string{imageName}}
	if tag != "" {
		q.Set("tag", tag)
	}
	return m.stream(ctx, "/images/create", q)
}

// splitImageRef splits "name:tag" at the last colon, tolerating a digest
// reference (name@sha256:...) where the colon belongs to the digest.
func splitImageRef(ref string) (name, tag string) {
	if name, rest, ok := strings.Cut(ref, "@"); ok {
		_ = rest
		return name, ""
	}
	if name, tag, ok := strings.CutLast(ref, ":"); ok {
		return name, tag
	}
	return ref, ""
}

// Events streams daemon events matching the filters until ctx is cancelled
// or the connection breaks.
func (m *miniClient) Events(ctx context.Context, filters map[string][]string) (<-chan Event, <-chan error) {
	eventCh := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		defer close(eventCh)
		q := url.Values{"filters": []string{encodeFilters(filters)}}
		body, err := m.stream(ctx, "/events", q)
		if err != nil {
			errCh <- err
			return
		}
		defer func() { _ = body.Close() }()
		decoder := json.NewDecoder(body)
		for {
			var ev Event
			if err := decoder.Decode(&ev); err != nil {
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					errCh <- err
				}
				return
			}
			select {
			case eventCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return eventCh, errCh
}

// Close releases the client's resources. The underlying transport holds the
// socket connections, which die with the process; nothing to close early.
func (m *miniClient) Close() error { return nil }

// do performs one API call. A 409 is returned as *conflictError; other
// non-2xx responses become errors carrying the daemon's message.
func (m *miniClient) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	u := url.URL{Scheme: "http", Host: m.host, Path: path}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusConflict:
		return &conflictError{op: method + " " + path}
	case resp.StatusCode >= 300:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

// stream performs one call whose body is a long-lived stream; the caller
// consumes and closes it.
func (m *miniClient) stream(ctx context.Context, path string, query url.Values) (io.ReadCloser, error) {
	u := url.URL{Scheme: "http", Host: m.host, Path: path}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp.Body, nil
}

// WatchEvents streams container start events until ctx is done. Resolving the
// engine exports DOCKER_HOST, which the client endpoint reads.
func WatchEvents(ctx context.Context) (<-chan Event, <-chan error, error) {
	cli, err := newMiniClient()
	if err != nil {
		return nil, nil, err
	}
	eventCh, errCh := cli.Events(ctx, map[string][]string{
		"type":  {"container"},
		"event": {"start"},
	})
	return eventCh, errCh, nil
}
