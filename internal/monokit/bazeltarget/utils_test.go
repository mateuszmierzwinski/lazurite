package bazeltarget

import (
	"errors"
	"strings"
	"testing"
)

// query is a slice of the operator's own monorepo, as bazel prints it.
const query = `go_binary rule //src/go/apps/lazurite/cmd/lazurite:lazurite
go_library rule //src/go/apps/lazurite/internal/ui:ui
go_test rule //src/go/apps/lazurite/internal/ui:ui_test
go_binary rule //src/go/tools/basalt/cmd/basalt:basalt
genrule rule //src/go/tools/basalt:basalt_rpm
Loading: 12 packages loaded
go_test rule //src/go/lib/ollama:ollama_test`

func labels(targets []Target) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Label)
	}
	return out
}

func TestALineIsReadAsBasaltReadsIt(t *testing.T) {
	got, err := ParseLabelKind("go_binary rule //src/go/tools/basalt/cmd/basalt:basalt")
	if err != nil {
		t.Fatal(err)
	}
	want := Target{Kind: "go_binary", Package: "//src/go/tools/basalt/cmd/basalt", Name: "basalt",
		Label: "//src/go/tools/basalt/cmd/basalt:basalt", Display: "[basalt:basalt]"}
	if got != want {
		t.Fatalf("parsed %+v", got)
	}
}

func TestWhatIsNotATargetLineIsRefused(t *testing.T) {
	for _, line := range []string{"", "Loading: 12 packages", "go_binary rule nolabel",
		"go_binary rule //pkg:", "go_binary rule //pkg:a:b", "go_binary source //pkg:x"} {
		if _, err := ParseLabelKind(line); err == nil {
			t.Fatalf("%q was read as a target", line)
		}
	}
	if _, err := ParseLabelKind("x rule nolabel"); !errors.Is(err, ErrMalformedLabel) {
		t.Fatalf("a bad label reported %v", err)
	}
}

// One stray notice must not cost the listing.
func TestParseSkipsWhatIsNotATarget(t *testing.T) {
	if got := Parse(query); len(got) != 6 {
		t.Fatalf("%d targets from %v", len(got), labels(got))
	}
}

func TestTheVerbFollowsTheKind(t *testing.T) {
	for kind, want := range map[Kind]Verb{
		"go_binary": VerbRun, "cc_binary": VerbRun, "go_test": VerbTest, "sh_test": VerbTest,
		"go_library": VerbBuild, "genrule": VerbBuild, "pkg_rpm": VerbBuild,
	} {
		if got := VerbOf(Target{Kind: kind}); got != want {
			t.Fatalf("%s is %q, want %q", kind, got, want)
		}
	}
}

// A label reaches bazel as ONE argument: a target named with a shell
// metacharacter is a name, never a second command (the run adapter has no
// shell, and this keeps the configuration from ever pretending otherwise).
func TestAPlainQueryIsASubstringInAnyCase(t *testing.T) {
	got, _ := Find(Parse(query), "BASALT", "")
	if strings.Join(labels(got), " ") != "//src/go/tools/basalt/cmd/basalt:basalt //src/go/tools/basalt:basalt_rpm" {
		t.Fatalf("found %v", labels(got))
	}
}

// A pattern is a pattern, as basalt accepts "(.*)falcon(.*)".
func TestAQueryWithAnOperatorIsARegularExpression(t *testing.T) {
	got, _ := Find(Parse(query), "(.*)lazurite(.*)_test$", "")
	if len(got) != 1 || got[0].Label != "//src/go/apps/lazurite/internal/ui:ui_test" {
		t.Fatalf("found %v", labels(got))
	}
}

// A half-typed pattern searches literally rather than emptying the list.
func TestAHalfTypedPatternIsSearchedLiterally(t *testing.T) {
	got, err := Find([]Target{{Label: "//x:weird(name"}}, "weird(", "")
	if err != nil || len(got) != 1 {
		t.Fatalf("found %v, %v", labels(got), err)
	}
}

// The open project's targets come first, and runnable before buildable.
func TestTheOpenProjectComesFirstAndRunnableBeforeBuildable(t *testing.T) {
	got, _ := Find(Parse(query), "", "src/go/apps/lazurite")
	want := []string{
		"//src/go/apps/lazurite/cmd/lazurite:lazurite",
		"//src/go/apps/lazurite/internal/ui:ui_test",
		"//src/go/apps/lazurite/internal/ui:ui",
	}
	if strings.Join(labels(got)[:3], " ") != strings.Join(want, " ") {
		t.Fatalf("ranked %v", labels(got))
	}
}

func TestAnOversizedQueryIsRefused(t *testing.T) {
	if _, err := Find(nil, strings.Repeat("a", MaxQueryLength+1), ""); !errors.Is(err, ErrQueryTooLong) {
		t.Fatalf("got %v", err)
	}
}

func TestAListingIsCapped(t *testing.T) {
	many := make([]Target, MaxRows+50)
	for i := range many {
		many[i] = Target{Kind: "go_library", Label: "//x:t"}
	}
	if got, _ := Find(many, "", ""); len(got) != MaxRows {
		t.Fatalf("%d rows", len(got))
	}
}

func BenchmarkParseLabelKind(b *testing.B) {
	for b.Loop() {
		_, _ = ParseLabelKind("go_binary rule //src/go/tools/basalt/cmd/basalt:basalt")
	}
}

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		Parse(query)
	}
}

func BenchmarkVerbOf(b *testing.B) {
	t := Target{Kind: "go_test"}
	for b.Loop() {
		VerbOf(t)
	}
}

func BenchmarkFind(b *testing.B) {
	targets := Parse(query)
	for b.Loop() {
		_, _ = Find(targets, "(.*)lazurite(.*)", "src/go/apps/lazurite")
	}
}

// TestAVerbIsAppliedToTheChosenTarget — the run bar's Ctrl+B builds whatever is
// selected, even a test, and Ctrl+T tests it. What a target means BY ITSELF is
// ConfigFor's question, and is still what picking one from the list does.

func TestTheQueryAsksForEveryRuleAndKeepsGoing(t *testing.T) {
	got := strings.Join(QueryArgs(), " ")
	if got != "query kind(rule, //...:*) --output=label_kind --keep_going" {
		t.Fatalf("got %q", got)
	}
	if len(Markers()) != 3 || Markers()[0] != "MODULE.bazel" || ExitPartial != 3 {
		t.Fatal("the markers or the partial exit changed")
	}
}

func TestADirectoryIsNamedAsALabelAndAsPatterns(t *testing.T) {
	for rel, want := range map[string]string{"src/go/x": "//src/go/x", "": "//", ".": "//", "/a/b/": "//a/b", `a\b`: "//a/b"} {
		if got := PackageLabel(rel); got != want {
			t.Errorf("PackageLabel(%q) = %q, want %q", rel, got, want)
		}
	}
	if Everything("//src/go/x") != "//src/go/x/..." || Everything("//") != "//..." || AllIn("//src/go/x") != "//src/go/x:all" {
		t.Fatal("the patterns are wrong")
	}
}

func BenchmarkPackageLabel(b *testing.B) {
	for b.Loop() {
		PackageLabel("src/go/apps/lazurite")
	}
}

func BenchmarkEverything(b *testing.B) {
	for b.Loop() {
		Everything("//src/go/apps/lazurite")
	}
}

func BenchmarkAllIn(b *testing.B) {
	for b.Loop() {
		AllIn("//src/go/apps/lazurite")
	}
}

func BenchmarkQueryArgs(b *testing.B) {
	for b.Loop() {
		QueryArgs()
	}
}

func BenchmarkMarkers(b *testing.B) {
	for b.Loop() {
		Markers()
	}
}
