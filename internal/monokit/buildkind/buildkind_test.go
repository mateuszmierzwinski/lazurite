package buildkind

import "testing"

func TestWhatAProjectIsBuiltWith(t *testing.T) {
	for _, c := range []struct {
		entries []string
		want    Kind
	}{
		{[]string{"go.mod", "main.go"}, Go},
		{[]string{"go.mod", "BUILD"}, Bazel},
		{[]string{"BUILD.bazel"}, Bazel},
		{[]string{"MODULE.bazel", "go.work"}, Bazel},
		{[]string{"WORKSPACE"}, Bazel},
		{[]string{"WORKSPACE.bazel"}, Bazel},
		{[]string{"WORKSPACE.bzlmod"}, Bazel},
		{[]string{"notes.txt"}, Unknown},
		{nil, Unknown},
	} {
		if got := Of(c.entries); got != c.want {
			t.Errorf("Of(%v) = %q, want %q", c.entries, got, c.want)
		}
	}
}

func BenchmarkOf(b *testing.B) {
	entries := []string{"go.mod", "main.go", "README.md", "BUILD", "cmd", "internal"}
	for b.Loop() {
		Of(entries)
	}
}
