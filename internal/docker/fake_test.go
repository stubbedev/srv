package docker

import (
	"context"
	"errors"
	"io"
	"strings"
)

// fakeSDK is a controllable sdkClient used by docker package tests.
type fakeSDK struct {
	closed bool

	pingErr error

	networks  []networkSummary
	listErr   error
	createErr error
	// createConflict makes NetworkCreate return the daemon's 409 class.
	createConflict bool
	createCount    int
	removeErr      error

	connectErr   error
	connectCount int

	inspect    map[string]inspectResponse
	inspectErr map[string]error

	listContainers     []containerSummary
	listContainersErr  error
	listContainersCall int

	pullReader io.ReadCloser
	pullErr    error
}

func (f *fakeSDK) Ping(ctx context.Context) error {
	return f.pingErr
}

func (f *fakeSDK) NetworkList(ctx context.Context, nameFilter string) ([]networkSummary, error) {
	return f.networks, f.listErr
}

func (f *fakeSDK) NetworkCreate(ctx context.Context, name, driver string) error {
	f.createCount++
	if f.createErr != nil {
		return f.createErr
	}
	if f.createConflict {
		return &conflictError{op: "network create"}
	}
	return nil
}

func (f *fakeSDK) NetworkRemove(ctx context.Context, name string) error {
	return f.removeErr
}

func (f *fakeSDK) NetworkConnect(ctx context.Context, networkName, containerID string, aliases []string) error {
	f.connectCount++
	return f.connectErr
}

func (f *fakeSDK) ContainerInspect(ctx context.Context, name string) (inspectResponse, error) {
	if err, ok := f.inspectErr[name]; ok {
		return inspectResponse{}, err
	}
	if r, ok := f.inspect[name]; ok {
		return r, nil
	}
	return inspectResponse{}, errors.New("not found")
}

func (f *fakeSDK) ContainerList(ctx context.Context, all bool, labelFilter string) ([]containerSummary, error) {
	f.listContainersCall++
	return f.listContainers, f.listContainersErr
}

func (f *fakeSDK) ImagePull(ctx context.Context, ref string) (io.ReadCloser, error) {
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	if f.pullReader != nil {
		return f.pullReader, nil
	}
	// Default to a minimal but well-formed pull JSON stream.
	return io.NopCloser(strings.NewReader(
		`{"status":"Pulling from library/nginx","id":""}` + "\n" +
			`{"status":"Downloading","id":"abc123","progress":"[===>]  1kB/2kB"}` + "\n" +
			`{"status":"Download complete","id":"abc123"}` + "\n" +
			`{"status":"Status: Downloaded newer image"}` + "\n")), nil
}

func (f *fakeSDK) Events(ctx context.Context, filters map[string][]string) (<-chan Event, <-chan error) {
	eventCh := make(chan Event)
	errCh := make(chan error, 1)
	close(eventCh)
	return eventCh, errCh
}

func (f *fakeSDK) Close() error {
	f.closed = true
	return nil
}
