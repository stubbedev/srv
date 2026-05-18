package docker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
)

func swap(t *testing.T, f *fakeSDK) {
	t.Helper()
	t.Cleanup(SwapNewClient(func() (sdkClient, error) { return f, nil }))
}

func swapErr(t *testing.T, err error) {
	t.Helper()
	t.Cleanup(SwapNewClient(func() (sdkClient, error) { return nil, err }))
}

func TestIndexByte(t *testing.T) {
	cases := []struct {
		in   []byte
		c    byte
		want int
	}{
		{[]byte("hello"), 'l', 2},
		{[]byte("hello"), 'x', -1},
		{[]byte(""), 'a', -1},
		{[]byte("abc"), 'a', 0},
	}
	for _, tt := range cases {
		if got := indexByte(tt.in, tt.c); got != tt.want {
			t.Errorf("indexByte(%q, %q) = %d, want %d", tt.in, tt.c, got, tt.want)
		}
	}
}

func TestPrefixWriterCompleteLine(t *testing.T) {
	var buf bytes.Buffer
	p := newPrefixWriter(&buf, "site")
	if _, err := p.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "[site] hello\n" {
		t.Errorf("got %q", got)
	}
}

func TestPrefixWriterPartialLineBuffered(t *testing.T) {
	var buf bytes.Buffer
	p := newPrefixWriter(&buf, "x")
	if _, err := p.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("partial line should buffer, got %q", buf.String())
	}
	if _, err := p.Write([]byte("-rest\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "[x] partial-rest\n" {
		t.Errorf("got %q", got)
	}
}

func TestPrefixWriterMultipleLinesOneWrite(t *testing.T) {
	var buf bytes.Buffer
	p := newPrefixWriter(&buf, "x")
	if _, err := p.Write([]byte("a\nb\nc\n")); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"[x] a\n", "[x] b\n", "[x] c\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestPrefixWriterReturnsLen(t *testing.T) {
	var buf bytes.Buffer
	p := newPrefixWriter(&buf, "x")
	n, err := p.Write([]byte("hello\nworld"))
	if err != nil {
		t.Fatal(err)
	}
	if n != len("hello\nworld") {
		t.Errorf("n = %d, want %d", n, len("hello\nworld"))
	}
}

func TestErrServiceNotRunning(t *testing.T) {
	if ErrServiceNotRunning == nil {
		t.Fatal("ErrServiceNotRunning nil")
	}
	if !strings.Contains(ErrServiceNotRunning.Error(), "not running") {
		t.Errorf("msg = %q", ErrServiceNotRunning.Error())
	}
}

func TestParseComposeStatusOutput(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", "stopped"},
		{"all-running", "Up 5 minutes\nUp 1 minute\n", "running"},
		{"all-stopped", "Exited (0)\nExited (0)\n", "stopped"},
		{"partial", "Up 5 minutes\nExited (0)\n", "partial (1/2)"},
		{"whitespace-only", "   \n\n", "stopped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseComposeStatusOutput(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestAggregateStatus(t *testing.T) {
	cases := []struct {
		running, total int
		want           string
	}{
		{0, 0, "stopped"},
		{2, 2, "running"},
		{1, 2, "partial (1/2)"},
		{0, 3, "stopped"},
	}
	for _, c := range cases {
		if got := aggregateStatus(c.running, c.total); got != c.want {
			t.Errorf("aggregateStatus(%d,%d) = %q, want %q", c.running, c.total, got, c.want)
		}
	}
}

func TestExtractImageTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"nginx:1.25", "1.25"},
		{"nginx", "latest"},
		{"", "latest"},
		{"repo/name:tag", "tag"},
		{"localhost:5000/repo:v1", "v1"},
	}
	for _, c := range cases {
		if got := extractImageTag(c.in); got != c.want {
			t.Errorf("extractImageTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnsureRunningOK(t *testing.T) {
	swap(t, &fakeSDK{})
	if err := EnsureRunning(); err != nil {
		t.Errorf("EnsureRunning err: %v", err)
	}
}

func TestEnsureRunningPingErr(t *testing.T) {
	swap(t, &fakeSDK{pingErr: errors.New("nope")})
	if err := EnsureRunning(); err == nil {
		t.Error("expected error from ping failure")
	}
}

func TestEnsureRunningClientFactoryErr(t *testing.T) {
	swapErr(t, errors.New("dial fail"))
	if err := EnsureRunning(); err == nil {
		t.Error("expected error from client factory")
	}
}

func TestNetworkExistsMatch(t *testing.T) {
	swap(t, &fakeSDK{networks: []network.Summary{{Name: "srv_traefik"}, {Name: "other"}}})
	if !NetworkExists("srv_traefik") {
		t.Error("expected match")
	}
}

func TestNetworkExistsPrefixOnly(t *testing.T) {
	swap(t, &fakeSDK{networks: []network.Summary{{Name: "srv_traefik_other"}}})
	if NetworkExists("srv_traefik") {
		t.Error("prefix match should not count as exact")
	}
}

func TestNetworkExistsClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if NetworkExists("srv_traefik") {
		t.Error("client error should yield false")
	}
}

func TestNetworkExistsListErr(t *testing.T) {
	swap(t, &fakeSDK{listErr: errors.New("x")})
	if NetworkExists("srv_traefik") {
		t.Error("list err should yield false")
	}
}

func TestEnsureInitializedExisting(t *testing.T) {
	swap(t, &fakeSDK{networks: []network.Summary{{Name: "net"}}})
	if err := EnsureInitialized("net"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestEnsureInitializedMissing(t *testing.T) {
	swap(t, &fakeSDK{})
	if err := EnsureInitialized("net"); err == nil {
		t.Error("expected error")
	}
}

func TestCreateNetworkSuccess(t *testing.T) {
	f := &fakeSDK{}
	swap(t, f)
	if err := CreateNetwork("net"); err != nil {
		t.Errorf("err: %v", err)
	}
	if f.createCount != 1 {
		t.Errorf("createCount = %d", f.createCount)
	}
}

func TestCreateNetworkConflictNoOp(t *testing.T) {
	swap(t, &fakeSDK{createErr: cerrdefs.ErrConflict})
	if err := CreateNetwork("net"); err != nil {
		t.Errorf("conflict should be no-op, got %v", err)
	}
}

func TestCreateNetworkOtherErr(t *testing.T) {
	swap(t, &fakeSDK{createErr: errors.New("boom")})
	if err := CreateNetwork("net"); err == nil {
		t.Error("expected propagated err")
	}
}

func TestCreateNetworkClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if err := CreateNetwork("net"); err == nil {
		t.Error("expected client err")
	}
}

func TestRemoveNetwork(t *testing.T) {
	swap(t, &fakeSDK{})
	if err := RemoveNetwork("net"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRemoveNetworkErr(t *testing.T) {
	swap(t, &fakeSDK{removeErr: errors.New("x")})
	if err := RemoveNetwork("net"); err == nil {
		t.Error("expected err")
	}
}

func TestRemoveNetworkClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if err := RemoveNetwork("net"); err == nil {
		t.Error("expected client err")
	}
}

func TestIsContainerRunningTrue(t *testing.T) {
	swap(t, &fakeSDK{inspect: map[string]container.InspectResponse{
		"x": {ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}}},
	}})
	if !IsContainerRunning("x") {
		t.Error("expected true")
	}
}

func TestIsContainerRunningStopped(t *testing.T) {
	swap(t, &fakeSDK{inspect: map[string]container.InspectResponse{
		"x": {ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: false}}},
	}})
	if IsContainerRunning("x") {
		t.Error("expected false")
	}
}

func TestIsContainerRunningMissing(t *testing.T) {
	swap(t, &fakeSDK{inspectErr: map[string]error{"x": errors.New("not found")}})
	if IsContainerRunning("x") {
		t.Error("missing container -> false")
	}
}

func TestIsContainerRunningClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if IsContainerRunning("x") {
		t.Error("client err -> false")
	}
}

func TestContainerExists(t *testing.T) {
	swap(t, &fakeSDK{inspect: map[string]container.InspectResponse{"x": {}}})
	if !ContainerExists("x") {
		t.Error("expected true")
	}
}

func TestContainerExistsMissing(t *testing.T) {
	swap(t, &fakeSDK{inspectErr: map[string]error{"x": errors.New("missing")}})
	if ContainerExists("x") {
		t.Error("expected false")
	}
}

func TestGetContainerImageVersion(t *testing.T) {
	swap(t, &fakeSDK{inspect: map[string]container.InspectResponse{
		"x": {Config: &container.Config{Image: "nginx:1.25"}},
	}})
	if got := GetContainerImageVersion("x"); got != "1.25" {
		t.Errorf("got %q", got)
	}
}

func TestGetContainerImageVersionUntagged(t *testing.T) {
	swap(t, &fakeSDK{inspect: map[string]container.InspectResponse{
		"x": {Config: &container.Config{Image: "nginx"}},
	}})
	if got := GetContainerImageVersion("x"); got != "latest" {
		t.Errorf("got %q", got)
	}
}

func TestGetContainerImageVersionMissing(t *testing.T) {
	swap(t, &fakeSDK{})
	if got := GetContainerImageVersion("x"); got != "" {
		t.Errorf("missing -> %q, want empty", got)
	}
}

func TestGetContainerImageVersionClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if got := GetContainerImageVersion("x"); got != "" {
		t.Errorf("client err -> %q", got)
	}
}

func TestPullSuccess(t *testing.T) {
	swap(t, &fakeSDK{})
	if err := Pull("nginx:latest"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestPullErr(t *testing.T) {
	swap(t, &fakeSDK{pullErr: errors.New("network")})
	if err := Pull("nginx:latest"); err == nil {
		t.Error("expected err")
	}
}

func TestPullClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if err := Pull("nginx:latest"); err == nil {
		t.Error("expected err")
	}
}

func TestConnectContainerToNetwork(t *testing.T) {
	f := &fakeSDK{}
	swap(t, f)
	if err := ConnectContainerToNetwork("c", "n", "alias"); err != nil {
		t.Errorf("err: %v", err)
	}
	if f.connectCount != 1 {
		t.Errorf("connectCount = %d", f.connectCount)
	}
}

func TestConnectContainerToNetworkConflictNoOp(t *testing.T) {
	swap(t, &fakeSDK{connectErr: cerrdefs.ErrConflict})
	if err := ConnectContainerToNetwork("c", "n", ""); err != nil {
		t.Errorf("conflict should be no-op, got %v", err)
	}
}

func TestConnectContainerToNetworkErr(t *testing.T) {
	swap(t, &fakeSDK{connectErr: errors.New("boom")})
	if err := ConnectContainerToNetwork("c", "n", ""); err == nil {
		t.Error("expected propagated err")
	}
}

func TestConnectContainerToNetworkClientErr(t *testing.T) {
	swapErr(t, errors.New("x"))
	if err := ConnectContainerToNetwork("c", "n", ""); err == nil {
		t.Error("expected err")
	}
}

func TestContainerStatusByNameRunning(t *testing.T) {
	swap(t, &fakeSDK{inspect: map[string]container.InspectResponse{
		"x": {ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}}},
	}})
	if got := ContainerStatusByName("x"); got != "running" {
		t.Errorf("got %q", got)
	}
}

func TestContainerStatusByNameMissing(t *testing.T) {
	swap(t, &fakeSDK{})
	if got := ContainerStatusByName("x"); got != "stopped" {
		t.Errorf("got %q", got)
	}
}

func TestContainerStatusByComposeDirEmpty(t *testing.T) {
	swap(t, &fakeSDK{})
	if got := ContainerStatusByComposeDir("/srv/x"); got != "stopped" {
		t.Errorf("got %q", got)
	}
}

func TestContainerStatusByComposeDirRunning(t *testing.T) {
	swap(t, &fakeSDK{listContainers: []container.Summary{
		{State: "running"},
		{State: "running"},
	}})
	if got := ContainerStatusByComposeDir("/srv/x"); got != "running" {
		t.Errorf("got %q", got)
	}
}

func TestContainerStatusByComposeDirPartial(t *testing.T) {
	swap(t, &fakeSDK{listContainers: []container.Summary{
		{State: "running"},
		{State: "exited"},
	}})
	if got := ContainerStatusByComposeDir("/srv/x"); got != "partial (1/2)" {
		t.Errorf("got %q", got)
	}
}

func TestExecDelegates(t *testing.T) {
	var captured []string
	t.Cleanup(SwapDockerExec(func(interactive bool, args ...string) error {
		captured = append([]string(nil), args...)
		_ = interactive
		return nil
	}))
	if err := Exec("blog", "sh"); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 4 || captured[0] != "exec" || captured[2] != "blog" {
		t.Errorf("captured = %v", captured)
	}
}

func TestExecNonInteractiveAtWorkDir(t *testing.T) {
	var captured []string
	t.Cleanup(SwapDockerExec(func(_ bool, args ...string) error {
		captured = append([]string(nil), args...)
		return nil
	}))
	if err := ExecNonInteractiveAt("blog", "/var/www", "ls"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(captured, " ")
	if !strings.Contains(joined, "-w /var/www") {
		t.Errorf("missing workdir flag: %v", captured)
	}
}

func TestExecNonInteractiveNoWorkDir(t *testing.T) {
	var captured []string
	t.Cleanup(SwapDockerExec(func(_ bool, args ...string) error {
		captured = append([]string(nil), args...)
		return nil
	}))
	if err := ExecNonInteractive("blog", "ls"); err != nil {
		t.Fatal(err)
	}
	if captured[1] != "blog" {
		t.Errorf("captured = %v", captured)
	}
}

func TestComposePrefixedDelegates(t *testing.T) {
	called := false
	t.Cleanup(SwapComposePrefixedExec(func(string, string, ...string) error {
		called = true
		return nil
	}))
	if err := ComposePrefixed("/x", "blog", "logs"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("expected prefixed exec to fire")
	}
}

// The default* exec funcs shell out to a real `docker` binary. They may fail
// in CI but the call exercises the seam's default-implementation path.
func TestDefaultComposeExecExercise(t *testing.T) {
	_ = defaultComposeExec("/", true, "version")
}

func TestDefaultDockerExecExercise(t *testing.T) {
	_ = defaultDockerExec(false, "version")
}

func TestDefaultComposePSOutputExercise(t *testing.T) {
	_, _ = defaultComposePSOutput(".")
}

func TestDefaultComposeServiceIDLookupExercise(t *testing.T) {
	ctx := context.Background()
	_, _ = defaultComposeServiceIDLookup(ctx, ".", "x")
}

func TestDefaultComposePrefixedExecExercise(t *testing.T) {
	_ = defaultComposePrefixedExec("/", "x", "version")
}

type composeCall struct {
	dir   string
	quiet bool
	args  []string
}

func captureCompose(t *testing.T, err error) *[]composeCall {
	t.Helper()
	calls := []composeCall{}
	t.Cleanup(SwapComposeExec(func(dir string, quiet bool, args ...string) error {
		calls = append(calls, composeCall{dir: dir, quiet: quiet, args: append([]string(nil), args...)})
		return err
	}))
	return &calls
}

func TestComposeDelegates(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := Compose("/x", "ps"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %d", len(*calls))
	}
	c := (*calls)[0]
	if c.quiet || c.dir != "/x" || len(c.args) != 1 || c.args[0] != "ps" {
		t.Errorf("got %+v", c)
	}
}

func TestComposeQuietPassesQuietFlag(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeQuiet("/x", "ps"); err != nil {
		t.Fatal(err)
	}
	if !(*calls)[0].quiet {
		t.Error("quiet flag not set")
	}
}

func TestComposeUp(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeUp("/x"); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0].args
	if got[0] != "up" || got[1] != "-d" {
		t.Errorf("args = %v", got)
	}
}

func TestComposeUpBuild(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeUpBuild("/x"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join((*calls)[0].args, " ")
	if !strings.Contains(joined, "--build") {
		t.Errorf("args missing --build: %v", (*calls)[0].args)
	}
}

func TestComposeUpWithProfile(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeUpWithProfile("/x", "dev"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join((*calls)[0].args, " ")
	if !strings.Contains(joined, "--profile dev") {
		t.Errorf("missing profile flag: %v", (*calls)[0].args)
	}
}

func TestComposeUpBuildWithProfile(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeUpBuildWithProfile("/x", "prod"); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0].args
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--profile prod") || !strings.Contains(joined, "--build") {
		t.Errorf("missing flags: %v", args)
	}
}

func TestComposeUpWithProfileEmpty(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeUpWithProfile("/x", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join((*calls)[0].args, " "), "--profile") {
		t.Errorf("empty profile should not add flag: %v", (*calls)[0].args)
	}
}

func TestComposeDown(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeDown("/x"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].args[0] != "down" {
		t.Errorf("args = %v", (*calls)[0].args)
	}
}

func TestComposeStop(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeStop("/x"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].args[0] != "stop" {
		t.Errorf("args = %v", (*calls)[0].args)
	}
}

func TestComposeRestart(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeRestart("/x"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].args[0] != "restart" {
		t.Errorf("args = %v", (*calls)[0].args)
	}
}

func TestComposeQuietWithProfile(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeQuietWithProfile("/x", "dev", "ps"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join((*calls)[0].args, " ")
	if !strings.Contains(joined, "--profile dev") {
		t.Errorf("missing profile: %v", (*calls)[0].args)
	}
	if !(*calls)[0].quiet {
		t.Error("quiet expected")
	}
}

func TestComposeQuietWithProfileEmptyDelegates(t *testing.T) {
	calls := captureCompose(t, nil)
	if err := ComposeQuietWithProfile("/x", "", "ps"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join((*calls)[0].args, " "), "--profile") {
		t.Errorf("empty profile should not add flag")
	}
}

func TestComposeErrPropagates(t *testing.T) {
	_ = captureCompose(t, errors.New("boom"))
	if err := Compose("/x", "up"); err == nil {
		t.Error("expected propagated err")
	}
}

func TestConnectServiceToNetworkOK(t *testing.T) {
	t.Cleanup(SwapComposeServiceIDLookup(func(_ context.Context, dir, svc string) (string, error) {
		return "abc123", nil
	}))
	swap(t, &fakeSDK{})
	if err := ConnectServiceToNetwork("/x", "web", "netA"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestConnectServiceToNetworkLookupErr(t *testing.T) {
	t.Cleanup(SwapComposeServiceIDLookup(func(_ context.Context, dir, svc string) (string, error) {
		return "", errors.New("missing")
	}))
	if err := ConnectServiceToNetwork("/x", "web", "netA"); !errors.Is(err, ErrServiceNotRunning) {
		t.Errorf("expected ErrServiceNotRunning, got %v", err)
	}
}

func TestContainerStatusRunning(t *testing.T) {
	t.Cleanup(SwapComposePSOutput(func(string) ([]byte, error) {
		return []byte("Up 5 minutes\nUp 1 minute\n"), nil
	}))
	if got := ContainerStatus("/x"); got != "running" {
		t.Errorf("got %q", got)
	}
}

func TestContainerStatusErr(t *testing.T) {
	t.Cleanup(SwapComposePSOutput(func(string) ([]byte, error) {
		return nil, errors.New("compose ps fail")
	}))
	if got := ContainerStatus("/x"); got != "stopped" {
		t.Errorf("got %q", got)
	}
}

func TestContainerStatusPartial(t *testing.T) {
	t.Cleanup(SwapComposePSOutput(func(string) ([]byte, error) {
		return []byte("Up\nExited\n"), nil
	}))
	if got := ContainerStatus("/x"); got != "partial (1/2)" {
		t.Errorf("got %q", got)
	}
}

func TestConnectServiceToNetworkEmptyID(t *testing.T) {
	t.Cleanup(SwapComposeServiceIDLookup(func(_ context.Context, dir, svc string) (string, error) {
		return "", nil
	}))
	if err := ConnectServiceToNetwork("/x", "web", "netA"); !errors.Is(err, ErrServiceNotRunning) {
		t.Errorf("expected ErrServiceNotRunning, got %v", err)
	}
}

func TestSwapNewClientErr(t *testing.T) {
	prev := newClientFn
	restore := SwapNewClientErr(errors.New("x"))
	defer restore()
	_, err := newClientFn()
	if err == nil {
		t.Error("expected err")
	}
	restore()
	if &newClientFn == nil || newClientFn == nil {
		_ = prev
	}
}

func TestSwapNewClientWithNetwork(t *testing.T) {
	restore := SwapNewClientWithNetwork("mynet")
	defer restore()
	if !NetworkExists("mynet") {
		t.Error("expected mynet to exist")
	}
}

func TestNetworkFakeSDKListMatches(t *testing.T) {
	f := networkFakeSDK{networkName: "x"}
	out, err := f.NetworkList(context.Background(), network.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "x" {
		t.Errorf("got %v", out)
	}
}

func TestSwapNewClientOK(t *testing.T) {
	restore := SwapNewClientOK()
	defer restore()
	cli, err := newClientFn()
	if err != nil {
		t.Fatal(err)
	}
	if cli == nil {
		t.Fatal("nil client")
	}
	// Exercise every noopSDK method.
	if _, err := cli.Ping(nil); err != nil {
		t.Errorf("Ping err: %v", err)
	}
	if _, err := cli.NetworkList(nil, network.ListOptions{}); err != nil {
		t.Errorf("NetworkList err: %v", err)
	}
	if _, err := cli.NetworkCreate(nil, "x", network.CreateOptions{}); err != nil {
		t.Errorf("NetworkCreate err: %v", err)
	}
	if err := cli.NetworkRemove(nil, "x"); err != nil {
		t.Errorf("NetworkRemove err: %v", err)
	}
	if err := cli.NetworkConnect(nil, "n", "c", nil); err != nil {
		t.Errorf("NetworkConnect err: %v", err)
	}
	if _, err := cli.ContainerInspect(nil, "c"); err == nil {
		t.Error("expected ContainerInspect err")
	}
	if _, err := cli.ContainerList(nil, container.ListOptions{}); err != nil {
		t.Errorf("ContainerList err: %v", err)
	}
	r, err := cli.ImagePull(nil, "x", image.PullOptions{})
	if err != nil {
		t.Errorf("ImagePull err: %v", err)
	}
	if r != nil {
		_ = r.Close()
	}
	if err := cli.Close(); err != nil {
		t.Errorf("Close err: %v", err)
	}
}

// erringWriter returns an error on Write to exercise prefixWriter's error path.
type erringWriter struct{}

func (erringWriter) Write([]byte) (int, error) {
	return 0, errFakeIO
}

var errFakeIO = errFake("io fail")

type errFake string

func (e errFake) Error() string { return string(e) }

func TestPrefixWriterPropagatesError(t *testing.T) {
	p := newPrefixWriter(erringWriter{}, "x")
	_, err := p.Write([]byte("hello\n"))
	if err == nil {
		t.Error("expected propagated error")
	}
}
