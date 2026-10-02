package lsp

import "encoding/json"

// serverRequestHandler answers one method the server asks of us.
//
// A named struct per method in a package-scoped map (Rule 6, 6.3) rather than a
// switch: each answer is a decision about what this editor promises, and each
// one is independently testable.
type serverRequestHandler interface {
	// Handle answers the request. Exactly one of the two returns is
	// meaningful: a nil error means the result is the answer — including a nil
	// result, which is the protocol's `null`.
	Handle(params json.RawMessage) (any, *wireError)
}

// handleServerRequest answers a request the server made, or says we do not
// implement it.
func handleServerRequest(method string, params json.RawMessage) (any, *wireError) {
	handler, known := serverRequests[method]
	if !known {
		return nil, &wireError{Code: codeMethodNotFound, Message: "lazurite does not implement " + method}
	}
	return handler.Handle(params)
}

// serverRequests is what this client answers, built once at package scope
// (Rule 6.1, 6.2).
//
// Short on purpose. Every entry is a promise, and the alternative to a promise
// is `method not found` — which every server handles, because no client
// implements all of this.
var serverRequests = map[string]serverRequestHandler{
	"client/registerCapability":      acknowledge{},
	"client/unregisterCapability":    acknowledge{},
	"window/workDoneProgress/create": acknowledge{},
	"workspace/configuration":        configuration{},
	"workspace/applyEdit":            refuseEdit{},
}

// acknowledge answers `null`, which the protocol reads as "done".
//
// Dynamic registration is acknowledged rather than honoured: this client
// declares `dynamicRegistration: false` everywhere, and a server that
// registers anyway — several do — is better told the message arrived than left
// waiting on an answer that never comes.
type acknowledge struct{}

// Handle answers an explicit null.
//
// Explicit because the response's result field is `omitempty`: a nil `any`
// vanishes, and a response carrying neither a result nor an error is not a
// response at all — which a strict server is entitled to reject.
func (acknowledge) Handle(json.RawMessage) (any, *wireError) {
	return json.RawMessage("null"), nil
}

// configuration answers `workspace/configuration` with one empty settings
// object per item asked for.
//
// The count MATTERS: the protocol pairs the answers with the items positionally
// and a server given the wrong number cannot tell which setting it got. An
// empty object means "no configuration for that section", which is true — this
// client sends a server no settings, and the settings the operator has belong
// to lazurite's own config file rather than to a language server's schema.
type configuration struct{}

// Handle answers one empty object per requested item.
func (configuration) Handle(params json.RawMessage) (any, *wireError) {
	var request configurationParams
	if err := json.Unmarshal(params, &request); err != nil {
		return []configurationItemResult{}, nil
	}
	answers := make([]configurationItemResult, len(request.Items))
	return answers, nil
}

// refuseEdit answers `workspace/applyEdit` with an honest no.
//
// This client declares `applyEdit: false`, so a server should not send one at
// all. One that does gets a refusal rather than `method not found`, because the
// two mean different things: the first says the edit was not applied, and the
// second invites the server to conclude the client is broken. Applying a
// server-initiated workspace edit is T5.4's, and it is a real feature with a
// preview in front of it, not a silent write.
type refuseEdit struct{}

// Handle refuses, naming why.
func (refuseEdit) Handle(json.RawMessage) (any, *wireError) {
	return applyEditResult{
		Applied:       false,
		FailureReason: "lazurite does not apply server-initiated edits",
	}, nil
}

// configurationParams is what a server asks for settings with.
type configurationParams struct {
	Items []configurationItem `json:"items"`
}

// configurationItem is one settings section a server asked about.
type configurationItem struct {
	ScopeURI string `json:"scopeUri"`
	Section  string `json:"section"`
}

// configurationItemResult is the empty settings object one item is answered
// with.
type configurationItemResult struct{}

// applyEditResult is the answer to workspace/applyEdit.
type applyEditResult struct {
	Applied       bool   `json:"applied"`
	FailureReason string `json:"failureReason,omitempty"`
}
