package docker

import (
	"errors"
	"testing"
)

// ErrRealEngineInTest is returned when a test binary reaches a default seam
// implementation — one that would exec a real container engine.
//
// Every engine interaction in this package goes through a swappable seam
// (SwapComposeExec, SwapDockerExec, SwapComposePSOutput,
// SwapComposeServiceIDLookup, SwapNewClient) precisely so tests never touch a
// real daemon. A test that reaches the default anyway has simply forgotten to
// swap one, and the consequence is not a harmless no-op: the reload paths run
// `compose up -d --force-recreate` against the *test's* temporary SRV_ROOT,
// which recreates the developer's own srv_dns/srv_proxy containers with bind
// mounts pointing at a t.TempDir that is deleted seconds later. That leaves a
// running container serving from a directory that no longer exists, and it is
// invisible until DNS stops resolving.
//
// So the default implementations refuse rather than proceed. The error names
// the seam to swap.
var ErrRealEngineInTest = errors.New("docker: a test reached the real container engine")

// guardTestExec reports an error when called from a test binary, naming the
// operation and the seam that should have been swapped. It costs one bool
// check outside tests.
func guardTestExec(op, seam string) error {
	if !testing.Testing() {
		return nil
	}
	return &testGuardError{op: op, seam: seam}
}

type testGuardError struct{ op, seam string }

func (e *testGuardError) Error() string {
	return "docker: " + e.op + " reached the real container engine from a test — " +
		"swap the seam first (docker." + e.seam + ")"
}

func (e *testGuardError) Unwrap() error { return ErrRealEngineInTest }
