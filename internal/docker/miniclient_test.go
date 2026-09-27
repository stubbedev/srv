package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

// startEventDaemon serves /events with the given handler and points
// DOCKER_HOST at it.
func startEventDaemon(t *testing.T, events http.HandlerFunc) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/events", events)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("SRV_CONTAINER_ENGINE", "")
	os.Unsetenv("SRV_CONTAINER_ENGINE")
}

// An engine restart ends the event stream with a clean EOF. The client must
// report that on errCh (before closing eventCh) rather than just closing the
// channel, so consumers can tell "stream over" from "no events yet".
func TestEventsCleanEOFReportsStreamClosed(t *testing.T) {
	startEventDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Event{Actor: EventActor{Attributes: map[string]string{"name": "web"}}})
		// Returning ends the chunked body: the clean EOF a restart produces.
	})
	cli, err := newMiniClient()
	if err != nil {
		t.Fatal(err)
	}
	eventCh, errCh := cli.Events(context.Background(), nil)

	ev, ok := <-eventCh
	if !ok || ev.Actor.Attributes["name"] != "web" {
		t.Fatalf("first event = %v (ok=%v), want container web", ev, ok)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrEventStreamClosed) {
			t.Errorf("err = %v, want ErrEventStreamClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no error reported after the stream ended")
	}
	if _, ok := <-eventCh; ok {
		t.Error("eventCh still open after the stream ended")
	}
}

// Cancelling the context is a requested stop, not a stream failure.
func TestEventsCancelReportsNothing(t *testing.T) {
	startEventDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	cli, err := newMiniClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	eventCh, errCh := cli.Events(ctx, nil)
	cancel()
	select {
	case _, ok := <-eventCh:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("eventCh not closed after cancel")
	}
	select {
	case err := <-errCh:
		t.Errorf("err = %v after cancel, want none", err)
	default:
	}
}
