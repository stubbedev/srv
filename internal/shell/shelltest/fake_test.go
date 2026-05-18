package shelltest

import (
	"context"
	"errors"
	"testing"
)

func TestFakeRecordsCalls(t *testing.T) {
	f := New(map[string]Response{
		"ufw": {Out: []byte("Status: active"), Exists: true},
	})
	out, err := f.RunQuiet("ufw", "status")
	if err != nil {
		t.Fatalf("RunQuiet err: %v", err)
	}
	if string(out) != "Status: active" {
		t.Errorf("out = %q", out)
	}
	if !f.Exists("ufw") {
		t.Error("Exists should return true")
	}

	calls := f.Snapshot()
	if len(calls) != 2 {
		t.Fatalf("calls len = %d, want 2", len(calls))
	}
	if calls[0].Method != "RunQuiet" || calls[0].Name != "ufw" {
		t.Errorf("call[0] = %+v", calls[0])
	}
	if calls[1].Method != "Exists" {
		t.Errorf("call[1] = %+v", calls[1])
	}
}

func TestFakeDefaultResponse(t *testing.T) {
	want := errors.New("fail")
	f := &Fake{Default: Response{Err: want}}
	if err := f.Run("anything"); !errors.Is(err, want) {
		t.Errorf("Run err = %v, want %v", err, want)
	}
}

func TestFakeSudoKey(t *testing.T) {
	f := New(map[string]Response{
		"sudo:ufw":      {Out: []byte("ok"), Err: nil},
		"sudo:tee":      {Err: nil},
		"sudo:mkdir":    {Err: nil},
		"sudo:rm":       {Err: nil},
		"sudo:systemctl": {Err: nil},
	})

	if _, err := f.SudoRunQuiet("ufw", "status"); err != nil {
		t.Errorf("SudoRunQuiet err: %v", err)
	}
	if err := f.SudoRun("ufw", "allow", "80"); err != nil {
		t.Errorf("SudoRun err: %v", err)
	}
	if err := f.SudoWrite("/etc/x", "data"); err != nil {
		t.Errorf("SudoWrite err: %v", err)
	}
	if err := f.SudoMkdir("/etc/x"); err != nil {
		t.Errorf("SudoMkdir err: %v", err)
	}
	if err := f.SudoRemove("/etc/x"); err != nil {
		t.Errorf("SudoRemove err: %v", err)
	}
	if err := f.SudoSystemctl("start", "x"); err != nil {
		t.Errorf("SudoSystemctl err: %v", err)
	}
	if err := f.RunWithStdin("data", "cat"); err != nil {
		t.Errorf("RunWithStdin err: %v", err)
	}
}

func TestFakeContextCancellation(t *testing.T) {
	f := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.RunWithContext(ctx, "x"); err == nil {
		t.Error("expected ctx error")
	}
	if _, err := f.RunQuietWithContext(ctx, "x"); err == nil {
		t.Error("expected ctx error for quiet")
	}
}

func TestFakePort(t *testing.T) {
	f := New(map[string]Response{
		"port:80":    {InUse: true},
		"process:80": {Process: "nginx"},
	})
	inUse, err := f.CheckPort("80")
	if err != nil {
		t.Fatal(err)
	}
	if !inUse {
		t.Error("expected in use")
	}
	if _, _ = f.CheckPortOnAddr("127.0.0.1", "80"); false {
	}
	if got := f.IdentifyPortProcess("80"); got != "nginx" {
		t.Errorf("Process = %q", got)
	}
	if got := f.IdentifyPortProcess("99"); got != "" {
		t.Errorf("missing process key -> %q, want empty", got)
	}
}

func TestFakeSnapshotCopy(t *testing.T) {
	f := New(nil)
	_ = f.Run("a")
	snap1 := f.Snapshot()
	_ = f.Run("b")
	snap2 := f.Snapshot()
	if len(snap1) != 1 || len(snap2) != 2 {
		t.Errorf("snapshot lengths %d / %d", len(snap1), len(snap2))
	}
}
