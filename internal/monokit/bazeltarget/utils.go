package bazeltarget

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	labelPrefix   = "//"
	labelColon    = ":"
	queryRuleWord = "rule"
	queryFields   = 3

	// MaxQueryLength bounds a search. Go's regexp is RE2, so there is no
	// catastrophic backtracking to fear, but compiling an unbounded pattern
	// still costs time proportional to it — basalt's limit, kept.
	MaxQueryLength = 512

	// MaxRows caps a listing. A monorepo holds thousands of targets and a
	// palette shows a dozen; the order is by relevance, so what is cut is
	// what matched worst.
	MaxRows = 200

	// regexOperators mark a query as a regular expression. A label holds
	// none of them, so a query with one was typed as a pattern — "(.*)lazurite",
	// "_test$" — and a query without one is a plain name.
	regexOperators = `*+?()[]{}|^$\`
)

// ParseLabelKind reads one line of `bazel query --output=label_kind` —
// "go_binary rule //src/go/x:x" — basalt's rule exactly.
func ParseLabelKind(line string) (Target, error) {
	fields := strings.Fields(line)
	if len(fields) != queryFields || fields[1] != queryRuleWord {
		return Target{}, fmt.Errorf("%w: %q", ErrMalformedLine, line)
	}
	pkg, name, err := splitLabel(fields[2])
	if err != nil {
		return Target{}, err
	}
	return Target{
		Kind: Kind(fields[0]), Package: pkg, Name: name, Label: fields[2],
		Display: displayName(pkg, name),
	}, nil
}

// splitLabel divides "//path/to/pkg:name" into its package and name.
func splitLabel(label string) (string, string, error) {
	pkg, name, found := strings.Cut(label, labelColon)
	if !strings.HasPrefix(label, labelPrefix) || !found || name == "" ||
		strings.Contains(name, labelColon) {
		return "", "", fmt.Errorf("%w: %q", ErrMalformedLabel, label)
	}
	return pkg, name, nil
}

// displayName is basalt's short form: the package's last segment and the name.
func displayName(pkg, name string) string {
	segments := strings.Split(strings.TrimPrefix(pkg, labelPrefix), "/")
	return "[" + segments[len(segments)-1] + labelColon + name + "]"
}

// Parse reads a whole query's output, skipping what is not a target line:
// one stray notice must not cost the operator the listing.
func Parse(stdout string) []Target {
	var out []Target
	for _, line := range strings.Split(stdout, "\n") {
		if t, err := ParseLabelKind(line); err == nil {
			out = append(out, t)
		}
	}
	return out
}

// VerbOf is what doing a target means: a test is tested, a binary is run, and
// everything else — a library, a genrule, a package — is built.
func VerbOf(t Target) Verb {
	kind := string(t.Kind)
	switch {
	case strings.HasSuffix(kind, "_test"):
		return VerbTest
	case strings.HasSuffix(kind, "_binary"):
		return VerbRun
	default:
		return VerbBuild
	}
}

// Find is the targets matching a query, best first.
//
// Literal and case-insensitive by default, because most searches are a
// project's name; a query holding a regular-expression operator is a pattern,
// as basalt accepts "(.*)falcon(.*)". A pattern that does not compile — half
// typed — is searched for literally rather than refused, so the list does not
// empty itself on every keystroke of a regex being written.
func Find(targets []Target, query, home string) ([]Target, error) {
	if len(query) > MaxQueryLength {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrQueryTooLong, len(query), MaxQueryLength)
	}
	matches := matching(targets, query)
	rank(matches, home)
	if len(matches) > MaxRows {
		matches = matches[:MaxRows]
	}
	return matches, nil
}

// matching filters targets by a query, literally or by pattern.
func matching(targets []Target, query string) []Target {
	test := literalTest(query)
	if regexLike(query) {
		if pattern, err := regexp.Compile("(?i)" + query); err == nil {
			test = pattern.MatchString
		}
	}
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		if query == "" || test(t.Label) {
			out = append(out, t)
		}
	}
	return out
}

// literalTest is a case-insensitive substring match.
func literalTest(query string) func(string) bool {
	want := strings.ToLower(query)
	return func(label string) bool { return strings.Contains(strings.ToLower(label), want) }
}

// regexLike reports whether a query was typed as a pattern.
func regexLike(query string) bool { return strings.ContainsAny(query, regexOperators) }

// rank orders targets: the open project's own first, then what can be run or
// tested ahead of what can only be built, then by label.
func rank(targets []Target, home string) {
	sort.SliceStable(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if in := inHome(a, home); in != inHome(b, home) {
			return in
		}
		if doing := VerbOf(a) != VerbBuild; doing != (VerbOf(b) != VerbBuild) {
			return doing
		}
		return a.Label < b.Label
	})
}

// inHome reports whether a target lives in the open project.
func inHome(t Target, home string) bool {
	if home == "" {
		return false
	}
	pkg := strings.TrimPrefix(t.Package, labelPrefix)
	return pkg == home || strings.HasPrefix(pkg, home+"/")
}

// QueryArgs are the arguments that ask bazel for every rule in the workspace,
// with its kind.
//
// `//...` rather than basalt's `//src/...`: basalt is shaped to one monorepo,
// and an editor opens any workspace. --keep_going, because one package that
// fails to load — a missing repository in somebody's use_repo — must not cost
// the operator every other target; bazel then exits ExitPartial with the rest
// printed.
func QueryArgs() []string {
	return []string{"query", "kind(rule, //...:*)", "--output=label_kind", "--keep_going"}
}

// Markers are the files at a Bazel workspace's root.
func Markers() []string { return []string{"MODULE.bazel", "WORKSPACE.bazel", "WORKSPACE"} }

// PackageLabel is the label of a directory inside a workspace, given as its
// path relative to the workspace's root: "src/go/x" is "//src/go/x", and the
// root itself is "//".
func PackageLabel(rel string) string {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" || rel == "." {
		return labelPrefix
	}
	return labelPrefix + rel
}

// Everything is the pattern for every target at or under a package:
// "//src/go/x/...", and "//..." for the workspace's root.
func Everything(pkg string) string {
	if pkg == labelPrefix {
		return pkg + "..."
	}
	return pkg + "/..."
}

// AllIn is the pattern for every target of one package and no other:
// "//src/go/x:all".
func AllIn(pkg string) string { return pkg + labelColon + "all" }
