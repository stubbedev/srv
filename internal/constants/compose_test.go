package constants

import "testing"

func TestComposeProjectFor(t *testing.T) {
	cases := map[string]string{
		"start-local": "srv-site-start-local",
		"Blog":        "srv-site-blog", // lowercased (compose requires it)
		"my_app":      "srv-site-my_app",
	}
	for in, want := range cases {
		if got := ComposeProjectFor(in); got != want {
			t.Errorf("ComposeProjectFor(%q) = %q, want %q", in, got, want)
		}
	}
	// Per-stack projects must be distinct from each other and from the legacy
	// shared project, or the whole point (no cross-stack orphans) is lost.
	if ComposeProjectFor("start-local") == ComposeProjectName ||
		MetricsComposeProject == ComposeProjectName ||
		ComposeProjectFor("metrics") == MetricsComposeProject {
		t.Error("stack projects must be distinct from the legacy shared project and each other")
	}
}
