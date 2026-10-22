package ide

import (
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// Completion: asking what could be typed here, offering it, and taking one.

// complete asks for the completions of the word at the caret.
func (a *App) complete() {
	d := a.doc()
	if d == nil {
		return
	}
	_, from := d.ed.Word()
	a.sync()
	path := d.path
	a.asked = asked{path: path, from: from}
	if a.warming {
		a.say(loadingNotice)
	}
	a.intel.Complete(path, d.ed.Caret, func(items []Suggestion) {
		a.post(func() { a.offered(path, from, items) })
	})
}

// completeNow asks for completions because the user asked: an answer with
// nothing in it is then said, rather than passed over in silence.
func (a *App) completeNow() {
	a.wanted = true
	a.complete()
}

// offered takes an answer. One that arrives after the caret has left the
// word it was asked for is dropped: it is about somewhere else.
func (a *App) offered(path string, from edit.Pos, items []Suggestion) {
	a.warmed()
	d := a.doc()
	if d == nil || d.path != path || a.dialog != nil || a.menu.Active {
		return
	}
	if _, now := d.ed.Word(); now != from {
		return
	}
	if len(items) == 0 && a.wanted {
		a.say("No completions here")
	}
	a.wanted = false
	if len(items) > maxSuggestions {
		items = items[:maxSuggestions]
	}
	a.popup = &popup{all: items, from: from}
	a.narrow()
}

// warmed notes that the language server has answered: it is loaded, and
// the first time that is worth saying — in place of the notice that it was
// loading, or of nothing. It arrives whenever the server is done, and
// whatever else the status line is saying then was said about something
// the user did, and stays.
func (a *App) warmed() {
	if !a.warming {
		return
	}
	a.warming = false
	if a.status == "" || a.status == loadingNotice {
		a.say(readyNotice)
	}
}

// narrow keeps the completions that still begin with what has been typed,
// and closes the list when none does or the caret has left the word.
func (a *App) narrow() {
	ed := a.editor()
	if a.popup == nil || ed == nil {
		a.popup = nil
		return
	}
	word, from := ed.Word()
	if from != a.popup.from {
		a.popup = nil
		return
	}
	a.popup.shown = beginning(a.popup.all, word)
	if len(a.popup.shown) == 0 {
		a.popup = nil
		return
	}
	a.popup.list.Set(labels(a.popup.shown))
}

// beginning is the suggestions whose label begins with a prefix, in any
// case.
func beginning(all []Suggestion, prefix string) []Suggestion {
	prefix = strings.ToLower(prefix)
	var out []Suggestion
	for _, s := range all {
		if strings.HasPrefix(strings.ToLower(s.Label), prefix) {
			out = append(out, s)
		}
	}
	return out
}

// labels is each suggestion as a line of the list: its name, and what it
// is.
func labels(all []Suggestion) []string {
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.Label
		if s.Detail != "" {
			out[i] += "  " + s.Detail
		}
	}
	return out
}

// followTyping keeps completion in step with a typed character: a
// character of a name narrows the list or asks for one; a dot asks afresh;
// anything else closes it.
func (a *App) followTyping(r rune) {
	d := a.doc()
	switch {
	case d == nil:
		a.popup = nil
	case r == '.':
		a.complete()
	case !identifier(r):
		a.popup = nil
	case a.popup != nil:
		a.narrow()
	default:
		a.askOnce(d)
	}
}

// askOnce asks for completions when the word being typed has not been
// asked about yet. A word is asked about once: the answer is narrowed as
// the word grows, rather than asked for again at every letter.
func (a *App) askOnce(d *document) {
	if _, from := d.ed.Word(); a.asked != (asked{path: d.path, from: from}) {
		a.complete()
	}
}

// popupKey gives a key to the completion list. It reports whether the key
// was the list's; one that was not closes the list and is handled as
// usual.
func (a *App) popupKey(ev term.Event) bool {
	switch {
	case a.popup.list.Key(ev, popupRows):
	case ev.Key == term.KeyEnter || ev.Key == term.KeyTab:
		a.accept()
	case ev.Key == term.KeyEsc:
		a.popup = nil
	case ev.Key == term.KeyBackspace && ev.Mods == 0:
		a.editor().Backspace()
		a.narrow()
	case ev.Key == term.KeyRune && ev.Mods == 0:
		return false
	default:
		a.popup = nil
		return false
	}
	return true
}

// accept takes the chosen completion: the word gives way to it, and
// whatever else it needs — an import — is added.
func (a *App) accept() {
	ed := a.editor()
	p := a.popup
	a.popup = nil
	if ed == nil || p.list.Sel >= len(p.shown) {
		return
	}
	s := p.shown[p.list.Sel]
	ed.Select(p.from, ed.Caret)
	ed.Insert(s.Insert)
	a.applyAround(s.Extra)
}

// applyAround makes edits elsewhere in the text and leaves the caret where
// it was in the text, not where it was on the page: an import added above
// moves the caret's line down with it.
func (a *App) applyAround(edits []Edit) {
	ed := a.editor()
	caret := ed.Caret
	for _, e := range latestFirst(edits) {
		ed.Buf.Replace(e.From, e.To, e.Text)
		if !caret.Before(e.To) {
			caret.Line += strings.Count(e.Text, "\n") - (e.To.Line - e.From.Line)
		}
	}
	ed.Goto(caret)
}

// latestFirst is edits ordered from the end of the text to its start, so
// that making one does not move the place of the next.
func latestFirst(edits []Edit) []Edit {
	out := append([]Edit(nil), edits...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].From.Before(out[j].From); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// define goes to where the name at the caret is defined.
func (a *App) define() {
	d := a.doc()
	if d == nil {
		return
	}
	a.sync()
	a.intel.Define(d.path, d.ed.Caret, func(p Place, ok bool) {
		a.post(func() { a.defined(p, ok) })
	})
}

// defined goes to a definition that was found.
func (a *App) defined(p Place, ok bool) {
	if !ok {
		a.say("No definition found")
		return
	}
	a.jump(p)
}

// describe shows what is known about the name at the caret.
func (a *App) describe() {
	d := a.doc()
	if d == nil {
		return
	}
	a.sync()
	a.intel.Describe(d.path, d.ed.Caret, func(text string) {
		a.post(func() { a.described(text) })
	})
}

// described shows a description, when there is one.
func (a *App) described(text string) {
	if strings.TrimSpace(text) == "" {
		a.say("Nothing is known about this")
		return
	}
	a.tell("Symbol", wrap(text, describeWidth)...)
}

// describeWidth is how wide a description is laid out.
const describeWidth = 64
