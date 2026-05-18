package cmd

import "testing"

func TestRunPaths(t *testing.T) {
	setupSrvRoot(t)
	if err := runPaths(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}
