package lsp

import (
	"context"
	"encoding/json"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// queryHandler asks one question and decodes its answer.
//
// A named struct per feature in a package-scoped map (Rule 6, 6.3) rather than
// a switch with thirteen arms: each is constructed and asserted on its own, and
// a fourteenth is a struct and a row rather than a branch in three places.
type queryHandler interface {
	// Handle sends the request and decodes what came back. The Feature field
	// of the answer is filled by Ask, so a handler never has to restate which
	// question it is.
	Handle(ctx context.Context, client language.Client, q language.Query) language.Answer
}

// Ask puts one question to a server and decodes the answer.
//
// SYNCHRONOUS, and deliberately: it is the protocol and nothing else. Who runs
// it off the frame thread, what deadline it gets and what is done with a path
// it names are the consumer's decisions, and `adapters/langsync` makes them.
//
// The CAPABILITY is checked before anything is sent. A server answers an
// unsupported method with a protocol error, which reaches the operator as a
// number; asking first turns that into "this server does not do hover".
func Ask(ctx context.Context, client language.Client, q language.Query) language.Answer {
	handler, known := queries[q.Feature]
	if !known || !language.Supports(client.Capabilities(), q.Feature) {
		return language.Answer{Feature: q.Feature, Err: language.ErrUnsupported}
	}
	answer := handler.Handle(ctx, client, q)
	answer.Feature = q.Feature
	return answer
}

// queries is every question this client knows how to ask (Rule 6.1, 6.2).
//
// Organise imports is a CODE ACTION of a particular kind rather than a request
// of its own, which is why it shares the handler and differs only in what it
// asks for.
var queries = map[language.Feature]queryHandler{
	language.FeatureDefinition:     places{method: methodDefinition},
	language.FeatureTypeDefinition: places{method: methodTypeDefinition},
	language.FeatureImplementation: places{method: methodImplementation},
	language.FeatureReferences:     usages{},
	language.FeatureHover:          prose{},
	language.FeatureSignature:      call{},
	language.FeatureHighlight:      occurrences{},
	language.FeaturePrepareRename:  renameable{},
	language.FeatureRename:         renaming{},
	language.FeatureFormat:         tidy{},
	language.FeatureFormatRange:    tidySelection{},
	language.FeatureCodeAction:     offers{},
	// A code action is TWO questions against a server that keeps its
	// refactorings back: what is on offer, and what the one that was chosen
	// actually changes.
	language.FeatureCodeActionResolve: resolvingAction{},
	// The two hierarchies: a prepare each, and two directions each
	// (hierarchy.go).
	language.FeaturePrepareCallHierarchy: preparing{method: methodPrepareCallHierarchy},
	language.FeatureIncomingCalls:        calling{method: methodIncomingCalls},
	language.FeatureOutgoingCalls:        calling{method: methodOutgoingCalls},
	language.FeaturePrepareTypeHierarchy: preparing{method: methodPrepareTypeHierarchy},
	language.FeatureSupertypes:           typing{method: methodSupertypes},
	language.FeatureSubtypes:             typing{method: methodSubtypes},
	language.FeatureOrganiseImports:      offers{only: []string{kindOrganiseImports}},
	// Completion is TWO questions rather than one: the list, and the one row
	// the operator stopped on (completion.go).
	language.FeatureCompletion:        suggestions{},
	language.FeatureCompletionResolve: resolving{},
}

// kindOrganiseImports is the protocol's name for the action that sorts an
// import block. Spelled the protocol's way, American `z` included, because it
// is a wire value rather than prose.
const kindOrganiseImports = "source.organizeImports"

// at is the document and point every navigation request carries.
func at(q language.Query) positionParams {
	return positionParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Position:     wirePositionOf(q.Position),
	}
}

// places answers the three "where is it" questions, which differ only in their
// method (Rule 5.1).
type places struct{ method string }

// Handle asks for locations.
func (p places) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var raw json.RawMessage
	if err := client.Request(ctx, p.method, at(q), &raw); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Locations: locationsOf(raw)}
}

// usages answers "where is this used", the declaration included.
type usages struct{}

// Handle asks for references.
func (usages) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	params := referenceParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Position:     wirePositionOf(q.Position),
		Context:      referenceContext{IncludeDeclaration: true},
	}
	var raw json.RawMessage
	if err := client.Request(ctx, methodReferences, params, &raw); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Locations: locationsOf(raw)}
}

// prose answers "what is this".
type prose struct{}

// Handle asks for hover.
func (prose) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer *wireHover
	if err := client.Request(ctx, methodHover, at(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Hover: hoverOf(answer)}
}

// call answers "what are the parameters of the call I am typing".
type call struct{}

// Handle asks for signature help.
func (call) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer *wireSignatureHelp
	if err := client.Request(ctx, methodSignatureHelp, at(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Signatures: signatureOf(answer)}
}

// occurrences answers "where else is this name in this file".
type occurrences struct{}

// Handle asks for document highlights.
func (occurrences) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer []wireHighlight
	if err := client.Request(ctx, methodHighlight, at(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Highlights: highlightsOf(answer)}
}

// renameable answers "can this be renamed, and what is it called now".
type renameable struct{}

// Handle asks prepareRename.
func (renameable) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer *wirePrepareRename
	if err := client.Request(ctx, methodPrepareRename, at(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Rename: prepareOf(answer)}
}

// renaming answers "what would renaming this change".
type renaming struct{}

// Handle asks for a rename's edits. Nothing is written here: the answer is a
// proposal, and what is done with it is the window's decision.
func (renaming) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	params := renameParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Position:     wirePositionOf(q.Position),
		NewName:      q.NewName,
	}
	var answer *wireWorkspaceEdit
	if err := client.Request(ctx, methodRename, params, &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Edit: workspaceEditOf(answer)}
}

// tidy answers "how should this whole document be laid out".
type tidy struct{}

// Handle asks for formatting.
func (tidy) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	params := formattingParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Options:      formatOptionsOf(q.Options),
	}
	var answer []wireTextEdit
	if err := client.Request(ctx, methodFormatting, params, &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Edits: editsOf(answer)}
}

// tidySelection answers the same question about a selection.
type tidySelection struct{}

// Handle asks for range formatting.
func (tidySelection) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	params := rangeFormattingParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Range:        spanOf(q.Span),
		Options:      formatOptionsOf(q.Options),
	}
	var answer []wireTextEdit
	if err := client.Request(ctx, methodRangeFormatting, params, &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Edits: editsOf(answer)}
}

// offers answers "what can be done about this", optionally narrowed to a kind.
type offers struct{ only []string }

// Handle asks for code actions.
func (o offers) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer []json.RawMessage
	if err := client.Request(ctx, methodCodeAction, o.params(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Actions: actionsOf(answer)}
}

// kinds are the action kinds to ask for: the handler's own where it has them —
// organise imports is one kind by definition — and otherwise whatever the
// QUERY named, which is how a refactoring becomes an action of its own.
func (o offers) kinds(q language.Query) []string {
	if len(o.only) > 0 {
		return o.only
	}
	return q.Only
}

// resolvingAction asks what the action the operator chose would change.
type resolvingAction struct{}

// Handle hands the server back its own offer and reads the edit it answers
// with.
//
// What came back KEEPS the title and kind it arrived with: resolve fills in an
// edit, and a server that echoed neither must not leave the operator agreeing
// to a change with no name on it.
func (resolvingAction) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer wireCodeAction
	if err := client.Request(ctx, methodCodeActionResolve, actionToWire(q.Action), &answer); err != nil {
		return language.Answer{Err: err}
	}
	filled := q.Action
	filled.Edit = workspaceEditOf(answer.Edit)
	return language.Answer{Action: filled}
}

// actionToWire puts an action back on the wire as the server sent it.
func actionToWire(action language.CodeAction) resolveCodeAction {
	return resolveCodeAction{
		Title: action.Title, Kind: action.Kind, Data: json.RawMessage(action.Data),
	}
}

// params is the code-action request, with the problems it should address.
func (o offers) params(q language.Query) codeActionParams {
	return codeActionParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Range:        spanOf(q.Span),
		Context: codeActionContext{
			Diagnostics: wireDiagnosticsOf(q.Diagnostics), Only: o.kinds(q),
		},
	}
}
