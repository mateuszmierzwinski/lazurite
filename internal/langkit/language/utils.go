package language

import (
	"path/filepath"
	"strings"
)

// Extension is a path's lower-cased extension, its leading dot included.
//
// The empty string for a path with no extension, which is a routing MISS
// rather than a key: a Makefile and a LICENCE must not collide on "".
func Extension(path string) string {
	return NormaliseExtension(filepath.Ext(path))
}

// NormaliseExtension puts an extension in the form the router keys on.
//
// Lower-cased, with exactly one leading dot, so a spec written ".GO", "GO" or
// ".go" claims the same files. A caller's configuration file is written by a
// human and this is where that is forgiven.
func NormaliseExtension(ext string) string {
	trimmed := strings.ToLower(strings.TrimSpace(ext))
	if trimmed == "" || trimmed == "." {
		return ""
	}
	return "." + strings.TrimPrefix(trimmed, ".")
}

// LanguageID is the protocol's identifier for a path's language.
//
// The protocol names these, and a server keys its behaviour on them, so
// inventing one is not an option — an unknown extension answers "plaintext",
// which every server accepts and none acts on.
func LanguageID(path string) string {
	if id, ok := languageIDs[Extension(path)]; ok {
		return id
	}
	return "plaintext"
}

// languageIDs maps an extension to the protocol's language identifier.
//
// Package-scoped and built once (Rule 6.2). The list is deliberately not
// Go-only: this client is a transport, and a Rust or Python server behind it
// must not need a change here to be told what it is looking at.
var languageIDs = map[string]string{
	".go": "go", ".mod": "go.mod", ".sum": "go.sum", ".work": "go.work",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp",
	".rs": "rust", ".py": "python", ".rb": "ruby", ".java": "java", ".kt": "kotlin",
	".js": "javascript", ".jsx": "javascriptreact", ".ts": "typescript",
	".tsx": "typescriptreact", ".json": "json", ".yaml": "yaml", ".yml": "yaml",
	".toml": "toml", ".html": "html", ".css": "css", ".md": "markdown",
	".sh": "shellscript", ".bash": "shellscript", ".sql": "sql", ".lua": "lua",
	".bzl": "starlark", ".bazel": "starlark", ".proto": "proto", ".xml": "xml",
}

// Confined resolves a path the SERVER named against the workspace root, and
// refuses anything outside it.
//
// A language server is a subprocess chosen by the user, but the paths in its
// answers are still input: a go-to-definition that opened whatever file the
// server pointed at would let a hostile or broken server steer the editor into
// `/etc/shadow` or up through `../../..`. The consumer decides what to do about
// a refusal; this is the one place the check is written.
//
// A path inside the root is returned cleaned and absolute. The root itself is
// inside itself, because a symbol may legitimately be in the workspace
// directory.
//
// SYMLINKS are resolved before the comparison, or a link inside the workspace
// pointing at `/etc` would let a server name anything on the machine while
// passing a lexical check — the classic escape from every sandbox that only
// counted `..` segments.
func Confined(root, path string) (string, error) {
	base, err := Resolved(root)
	if err != nil {
		return "", err
	}
	target, err := Resolved(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	return target, nil
}

// Resolved is an absolute path with its symbolic links followed as far as they
// actually exist.
//
// The "as far as they exist" is the point. A language server may legitimately
// name a file that has not been created yet — a generated one, a rename's
// destination — and EvalSymlinks refuses a path whose leaf is missing. Falling
// back to the lexical form there would leave the escape open through a
// symlinked PARENT, so the longest existing ancestor is resolved and the rest
// is re-attached to it.
func Resolved(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return realAncestor(abs), nil
}

// realAncestor resolves the longest existing prefix of an absolute path and
// puts the remainder back on the end of it.
func realAncestor(abs string) string {
	remainder := ""
	for at := abs; ; {
		if real, err := filepath.EvalSymlinks(at); err == nil {
			return filepath.Join(real, remainder)
		}
		parent := filepath.Dir(at)
		if parent == at {
			return abs
		}
		remainder = filepath.Join(filepath.Base(at), remainder)
		at = parent
	}
}

// Supports reports whether a server's capabilities cover a feature.
//
// The ONE place the mapping is written, so a consumer asks rather than calling
// and failing (`Client.Capabilities`) and cannot come to disagree with the
// adapter about what a capability means.
//
// An unknown feature is UNSUPPORTED. The safe direction: a feature nobody has
// mapped is one nobody has implemented, and offering it would be a menu entry
// that answers with a protocol error.
func Supports(caps Capabilities, f Feature) bool { return covered(caps)[f] }

// covered is which features a capability set admits.
//
// A TABLE of values rather than a switch (Rule 6) — thirteen checks of one
// parameter is a map — and of values rather than predicates, because what is
// being looked up is a fact about this server and not a decision to be made
// later.
//
// Organise imports asks for CODE ACTIONS, because that is what it is: an action
// of the kind `source.organizeImports`, not a request of its own.
func covered(c Capabilities) map[Feature]bool {
	return map[Feature]bool{
		FeatureDefinition:      c.Definition,
		FeatureTypeDefinition:  c.TypeDefinition,
		FeatureImplementation:  c.Implementation,
		FeatureReferences:      c.References,
		FeatureHover:           c.Hover,
		FeatureSignature:       c.SignatureHelp,
		FeatureHighlight:       c.DocumentHighlight,
		FeaturePrepareRename:   c.PrepareRename,
		FeatureRename:          c.Rename,
		FeatureFormat:          c.Formatting,
		FeatureFormatRange:     c.RangeFormatting,
		FeatureCodeAction:      c.CodeAction,
		FeatureOrganiseImports: c.CodeAction,
		// Resolve is its own capability here too, and asking a server that does
		// not offer it is how an editor turns "extract function" into a
		// protocol error number.
		FeatureCodeActionResolve: c.CodeActionResolve,
		// Each hierarchy is ONE capability covering its prepare and both
		// directions: the protocol has a single provider flag for each, and a
		// server offering the prepare and refusing the steps is not a shape it
		// allows.
		FeaturePrepareCallHierarchy: c.CallHierarchy,
		FeatureIncomingCalls:        c.CallHierarchy,
		FeatureOutgoingCalls:        c.CallHierarchy,
		FeaturePrepareTypeHierarchy: c.TypeHierarchy,
		FeatureSupertypes:           c.TypeHierarchy,
		FeatureSubtypes:             c.TypeHierarchy,
		FeatureCompletion:           c.Completion,
		// Resolve is its OWN capability rather than completion's. gopls offers
		// both; other servers offer completion and no resolve, and a client
		// that read one for the other would ask a question that comes back as
		// an error code every time the operator moves down a row.
		FeatureCompletionResolve: c.CompletionResolve,
	}
}
