package lsp

import (
	"encoding/json"
	"strconv"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// methodPublishDiagnostics is what a server sends unasked when it has looked at
// a file.
const methodPublishDiagnostics = "textDocument/publishDiagnostics"

// maxDiagnostics is how many problems one file may carry.
//
// A bound rather than trust: a server with a broken parser publishes one
// diagnostic per token, and a pane that listed a million of them is a window
// that stops responding. Far above what a genuinely broken file produces.
const maxDiagnostics = 1000

// Published decodes a publishDiagnostics notification, and says whether the
// notification was one at all.
//
// This is the seam `core/language.Sink` describes: the notification arrives as
// raw JSON because the core does not know the protocol, and this package does.
// Everything it returns has been through `language.CleanLine` — the message is
// a subprocess's prose, about to be drawn beside the operator's code.
func Published(n language.Notification) (language.Publication, bool) {
	if n.Method != methodPublishDiagnostics {
		return language.Publication{}, false
	}
	var params publishParams
	if err := json.Unmarshal(n.Params, &params); err != nil {
		return language.Publication{}, false
	}
	path, err := uriToPath(params.URI)
	if err != nil {
		return language.Publication{}, false
	}
	return language.Publication{
		Path: path, Version: params.Version, Diagnostics: diagnosticsOf(params.Diagnostics),
	}, true
}

// diagnosticsOf converts the wire form, bounded and scrubbed.
func diagnosticsOf(wire []wireDiagnostic) []language.Diagnostic {
	out := make([]language.Diagnostic, 0, min(len(wire), maxDiagnostics))
	for _, d := range wire[:min(len(wire), maxDiagnostics)] {
		out = append(out, language.Diagnostic{
			Range:    rangeOf(d.Range),
			Severity: severityOf(d.Severity),
			Code:     language.CleanLine(codeOf(d.Code)),
			Source:   language.CleanLine(d.Source),
			Message:  language.CleanLine(d.Message),
		})
	}
	return out
}

// severityOf reads the protocol's number, defaulting to an error.
//
// The protocol makes severity OPTIONAL and says a client may pick; picking
// "error" is the safe direction. A missing severity shown as a hint is a real
// problem the operator never sees.
func severityOf(n int) language.Severity {
	if n < int(language.SeverityError) || n > int(language.SeverityHint) {
		return language.SeverityError
	}
	return language.Severity(n)
}

// codeOf renders a diagnostic code, which the protocol allows to be a number
// or a string.
//
// Anything else — an object, an array, a server that invented a shape — is read
// as no code rather than as its raw JSON, which would put a brace in a column
// two characters wide.
func codeOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return ""
}

// rangeOf converts a span from the wire.
func rangeOf(r wireRange) language.Range {
	return language.Range{Start: positionOf(r.Start), End: positionOf(r.End)}
}

// positionOf converts a point from the wire, refusing a negative one.
//
// A negative line is not a position, and one reaching a decoration layer
// indexes behind the start of the buffer.
func positionOf(p wirePosition) language.Position {
	return language.Position{Line: max(0, p.Line), Character: max(0, p.Character)}
}
