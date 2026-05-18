package docker

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
)

// fakeSDK is a controllable sdkClient used by docker package tests.
type fakeSDK struct {
	closed bool

	pingErr error
	pingRet types.Ping

	networks    []network.Summary
	listErr     error
	createErr   error
	createCount int
	removeErr   error

	connectErr   error
	connectCount int

	inspect    map[string]container.InspectResponse
	inspectErr map[string]error

	listContainers     []container.Summary
	listContainersErr  error
	listContainersCall int

	pullReader io.ReadCloser
	pullErr    error
}

func (f *fakeSDK) Ping(ctx context.Context) (types.Ping, error) {
	return f.pingRet, f.pingErr
}

func (f *fakeSDK) NetworkList(ctx context.Context, opts network.ListOptions) ([]network.Summary, error) {
	return f.networks, f.listErr
}

func (f *fakeSDK) NetworkCreate(ctx context.Context, name string, opts network.CreateOptions) (network.CreateResponse, error) {
	f.createCount++
	if f.createErr != nil {
		return network.CreateResponse{}, f.createErr
	}
	return network.CreateResponse{ID: "net-id-" + name}, nil
}

func (f *fakeSDK) NetworkRemove(ctx context.Context, name string) error {
	return f.removeErr
}

func (f *fakeSDK) NetworkConnect(ctx context.Context, networkID, containerID string, cfg *network.EndpointSettings) error {
	f.connectCount++
	return f.connectErr
}

func (f *fakeSDK) ContainerInspect(ctx context.Context, name string) (container.InspectResponse, error) {
	if err, ok := f.inspectErr[name]; ok {
		return container.InspectResponse{}, err
	}
	if r, ok := f.inspect[name]; ok {
		return r, nil
	}
	return container.InspectResponse{}, errors.New("not found")
}

func (f *fakeSDK) ContainerList(ctx context.Context, opts container.ListOptions) ([]container.Summary, error) {
	f.listContainersCall++
	return f.listContainers, f.listContainersErr
}

func (f *fakeSDK) ImagePull(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	if f.pullReader != nil {
		return f.pullReader, nil
	}
	return io.NopCloser(strings.NewReader("pull progress\n")), nil
}

func (f *fakeSDK) Close() error {
	f.closed = true
	return nil
}
