package ide

import (
	"path/filepath"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ui"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/lexer"
)

// How the IDE is drawn: the whole picture every time, of which only what
// changed is sent to the terminal.

// Paint draws the IDE and sends the terminal what changed since the last
// frame.
func (a *App) Paint() error {
	a.draw()
	frame := term.Render(a.shown, a.screen)
	a.shown = a.screen.Clone()
	return a.deps.Terminal.Show(frame)
}

// The bounds of the desktop's parts.
const (
	minPaneHeight = 6
	maxPaneHeight = 14
	// The project tree takes a quarter of the width, within these bounds:
	// a wide terminal gives the extra to the text, which is what is being
	// read. Below minTreeScreen columns there is no room for both.
	minTreeWidth  = 20
	maxTreeWidth  = 32
	minTreeScreen = 70
)

// areas is where each part of the desktop is. A part that is not shown has
// no size.
type areas struct {
	tree, text, bottom term.Rect
}

// layout divides the desktop: the bottom window across its foot, the
// project tree down its left side, and the text in what is left.
func (a *App) layout() areas {
	desk := term.Rect{Y: 1, W: a.screen.W, H: max(a.screen.H-2, 0)}
	var out areas
	if a.pane.shown && desk.H >= minPaneHeight*2 {
		h := min(max(desk.H/3, minPaneHeight), maxPaneHeight)
		out.bottom = term.Rect{Y: desk.Y + desk.H - h, W: desk.W, H: h}
		desk.H -= h
	}
	if a.tree.shown && desk.W >= minTreeScreen {
		w := min(max(desk.W/4, minTreeWidth), maxTreeWidth)
		out.tree = term.Rect{Y: desk.Y, W: w, H: desk.H}
		desk.X, desk.W = w, desk.W-w
	}
	if len(a.docs) > 0 {
		out.text = desk
	}
	return out
}

// draw paints everything: desktop, windows, completion list, menu bar,
// status line and, over them all, the dialog.
func (a *App) draw() {
	s := a.screen
	s.CursorOn = false
	at := a.layout()
	ui.Backdrop(s, term.Rect{Y: 1, W: s.W, H: max(s.H-2, 0)})
	a.drawTree(at.tree)
	a.drawText(at.text)
	a.drawPane(at.bottom)
	if a.focus != focusText || a.menu.Active {
		s.CursorOn = false
	}
	a.drawPopup()
	ui.Status(s, a.hints(), a.status)
	a.menu.Draw(s)
	if a.dialog != nil {
		a.dialog.Draw(s)
	}
}

// kindFoot is what builds the project, as the tree's frame says it.
func (a *App) kindFoot() string {
	switch {
	case a.bazel() && a.proj.detached:
		return "bazel · GOWORK=off"
	case a.bazel():
		return "bazel"
	case a.proj.detached:
		return "go · GOWORK=off"
	}
	return "go"
}

// drawTree paints the project tree.
func (a *App) drawTree(r term.Rect) {
	if r.H < 3 {
		return
	}
	title := ui.Title{Name: filepath.Base(a.root), Active: a.focus == focusTree, Foot: a.kindFoot()}
	ui.Window(a.screen, r, title, ui.Pane)
	a.tree.list.Draw(a.screen, inner(r), ui.Pane, ui.PaneChosen, a.focus == focusTree)
}

// drawText paints the window of the text being edited.
func (a *App) drawText(r term.Rect) {
	d := a.doc()
	if d == nil || r.H < 3 {
		return
	}
	title := ui.Title{Name: d.name, Number: a.at + 1, Foot: caretFoot(d.ed), Changed: d.changed(), Active: a.focus == focusText}
	ui.Window(a.screen, r, title, ui.Text)
	ui.DrawEditor(a.screen, inner(r), d.ed, a.picture(d))
}

// picture is what the drawing of a text needs besides the text.
func (a *App) picture(d *document) ui.Picture {
	a.colour(d)
	p := ui.Picture{Spans: d.spans, Breaks: a.breaksIn(d.path), Resume: a.resumeLine(d.path), Free: a.scrolled}
	for _, pr := range a.problems[d.path] {
		p.Marks = append(p.Marks, ui.Mark{From: pr.From, To: pr.To, Severe: pr.Severe})
	}
	return p
}

// colour scans a text for its syntax when it has changed since it was last
// scanned.
func (a *App) colour(d *document) {
	rev := d.ed.Buf.Rev()
	if d.spans != nil && d.spansRev == rev {
		return
	}
	d.spansRev = rev
	lang, ok := lexer.For(d.name)
	if !ok {
		d.spans = [][]lexer.Span{}
		return
	}
	d.spans = ui.ByLine(lexer.Scan(lang, d.ed.Buf.Text()), d.ed.Buf.Lines())
}

// drawPane paints the bottom window and the page it shows.
func (a *App) drawPane(r term.Rect) {
	if r.H < 3 {
		return
	}
	title := ui.Title{Name: paneTitles[a.pane.kind], Active: a.focus == focusPane, Foot: "Alt+M Messages · Alt+O Output · Alt+V Debug"}
	ui.Window(a.screen, r, title, ui.Pane)
	list := &a.pane.lists[a.pane.kind]
	list.Draw(a.screen, inner(r), ui.Pane, ui.PaneChosen, a.focus == focusPane)
}

// drawPopup paints the completion list under the caret, or above it when
// there is no room below.
func (a *App) drawPopup() {
	p := a.popup
	s := a.screen
	if p == nil || !s.CursorOn {
		return
	}
	w := 12
	for _, line := range p.list.Items {
		w = max(w, len([]rune(line))+2)
	}
	w = min(w, min(60, s.W))
	h := min(len(p.list.Items), popupRows)
	r := term.Rect{X: max(min(s.CurX-1, s.W-w), 0), Y: s.CurY + 1, W: w, H: h}
	if r.Y+h > s.H-1 {
		r.Y = max(s.CurY-h, 1)
	}
	ui.Shadow(s, r)
	p.list.Draw(s, r, ui.Popup, ui.PopupChosen, true)
}

// The keys named on the status line, by what is going on.
var (
	hintsIdle = []ui.Hint{{Key: "F1", Label: "Help"}, {Key: "F2", Label: "Save"}, {Key: "F3", Label: "Open"},
		{Key: "F9", Label: "Build"}, {Key: "Ctrl+F9", Label: "Run"}, {Key: "F4", Label: "Debug"}, {Key: "Alt+T", Label: "Tree"}, {Key: "F10", Label: "Menu"}}
	hintsResting = []ui.Hint{{Key: "F8", Label: "Over"}, {Key: "F7", Label: "Into"}, {Key: "Shift+F8", Label: "Out"},
		{Key: "F4", Label: "Continue"}, {Key: "Ctrl+F2", Label: "Stop"}}
	hintsRunning = []ui.Hint{{Key: "Ctrl+F2", Label: "Stop"}, {Key: "Alt+O", Label: "Output"}}
	hintsPane    = []ui.Hint{{Key: "Enter", Label: "Go to"}, {Key: "Esc", Label: "Editor"}, {Key: "F5", Label: "Zoom"}}
	hintsTree    = []ui.Hint{{Key: "Enter", Label: "Open"}, {Key: "←→", Label: "Fold"}, {Key: "Esc", Label: "Editor"}, {Key: "Alt+T", Label: "Hide"}}
	hintsMenu    = []ui.Hint{{Key: "Enter", Label: "Choose"}, {Key: "Esc", Label: "Back"}}
)

// hints is the keys worth naming now.
func (a *App) hints() []ui.Hint {
	switch {
	case a.menu.Active:
		return hintsMenu
	case a.dbg.session.AtRest():
		return hintsResting
	case a.busy() || a.stop != nil:
		return hintsRunning
	case a.focus == focusTree:
		return hintsTree
	case a.focus == focusPane:
		return hintsPane
	}
	return hintsIdle
}

// Text is the picture as lines of text, without its colours: what a test
// reads.
func (a *App) Text() string {
	var sb strings.Builder
	for y := 0; y < a.screen.H; y++ {
		for x := 0; x < a.screen.W; x++ {
			sb.WriteRune(a.screen.At(x, y).R)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
