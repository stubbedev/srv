package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// startFakeDaemon serves a few API endpoints over a TCP listener and points
// DOCKER_HOST at it, so the HTTP layer of the minimal client is exercised
// end-to-end without a real runtime.
func startFakeDaemon(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/networks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]networkSummary{{Name: "srv_traefik"}})
	})
	mux.HandleFunc("/containers/web/json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(inspectResponse{
			State:  &inspectState{Running: true},
			Config: &inspectConfig{Image: "nginx:1.25"},
		})
	})
	mux.HandleFunc("/networks/newnet/connect", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "endpoint already exists", http.StatusConflict)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: mux},
	}
	srv.Start()
	t.Cleanup(srv.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
}

func TestMiniClientAgainstFakeDaemon(t *testing.T) {
	startFakeDaemon(t)
	t.Setenv("SRV_CONTAINER_ENGINE", "")
	os.Unsetenv("SRV_CONTAINER_ENGINE")

	cli, err := newMiniClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	networks, err := cli.NetworkList(context.Background(), "srv_traefik")
	if err != nil {
		t.Fatalf("NetworkList: %v", err)
	}
	if len(networks) != 1 || networks[0].Name != "srv_traefik" {
		t.Errorf("networks = %v", networks)
	}

	info, err := cli.ContainerInspect(context.Background(), "web")
	if err != nil {
		t.Fatalf("ContainerInspect: %v", err)
	}
	if !info.State.Running || info.Config.Image != "nginx:1.25" {
		t.Errorf("inspect = %+v", info)
	}

	// A 409 must come back as the portable conflict error, which the
	// idempotent connect path treats as success.
	err = cli.NetworkConnect(context.Background(), "newnet", "web", nil)
	if !IsConflict(err) {
		t.Errorf("expected conflict error, got %v", err)
	}
}
