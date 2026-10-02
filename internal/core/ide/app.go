package ide

import (
	"context"
	"unicode"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ui"
)

// What the status line says while the language server loads a project,
// and when it has.
const (
	loadingNotice = "The language server is loading the project — completion starts when it is ready"
	readyNotice   = "Language server ready"
)

// New builds the IDE. The context is its lifetime: when it ends, Run
// returns and everything the IDE started is stopped.
func New(ctx context.Context, deps Deps) *App {
	a := &App{
		deps: deps, ctx: ctx,
		// 256: see the field.
		posted:   make(chan func(), 256),
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
		problems: map[string][]Problem{},
		menu:     ui.MenuBar{Menus: menus()},
	}
	a.root, a.status, a.warming = deps.Root, deps.Notice, deps.Warming
	if a.warming && a.status == "" {
		a.status = loadingNotice
	}
	a.survey()
	a.intel = a.firstIntel()
	a.dbg.breaks, a.dbg.stale = map[string]map[int]bool{}, map[string]bool{}
	if deps.NewDebugger != nil {
		a.debugger = deps.NewDebugger(watcher{a: a})
	}
	a.intel.Listen(a.heard)
	w, h := deps.Terminal.Size()
	a.screen = term.NewScreen(w, h)
	a.tree, a.focus = tree{shown: true, open: map[string]bool{}}, focusTree
	a.rebuildTree()
	return a
}

// Run is the loop: draw, wait for a key or for work another goroutine
// posted, do it, draw again. It returns when the user quits, the terminal
// goes away or the IDE's context ends, having stopped what the IDE started.
func (a *App) Run() error {
	defer close(a.done)
	defer a.Shutdown()
	for !a.quit {
		if err := a.Paint(); err != nil {
			return err
		}
		select {
		case ev, ok := <-a.deps.Terminal.Events():
			if !ok {
				return nil
			}
			a.Handle(ev)
		case work := <-a.posted:
			work()
		case <-a.ctx.Done():
			return nil
		}
		a.Drain()
	}
	return nil
}

// Shutdown stops what the IDE started: the job, the debugger, the language
// server, its own worker. Run calls it on the way out; something that
// drives the IDE without Run calls it itself. Calling it twice is harmless.
func (a *App) Shutdown() {
	a.closing.Do(func() { close(a.stopped) })
	if a.stop != nil {
		a.stop()
	}
	if a.debugger != nil {
		_ = a.debugger.Stop(a.ctx)
	}
	a.intel.Stop()
}

// post hands work to the loop from another goroutine. Work posted after
// the loop has ended is dropped.
func (a *App) post(work func()) {
	select {
	case a.posted <- work:
	case <-a.done:
	}
}

// spawn runs work off the loop — in the order it was given, one piece at a
// time (see worker) — and the work reports back only through post.
func (a *App) spawn(work func()) {
	if a.deps.Spawn != nil {
		a.deps.Spawn(work)
		return
	}
	a.offLoop.Do(func() { a.work = newWorker(a.stopped) })
	a.work.do(work)
}

// Drain does the work that has been posted and is waiting, without
// blocking.
func (a *App) Drain() {
	for {
		select {
		case work := <-a.posted:
			work()
		default:
			return
		}
	}
}

// Quitting reports whether the user has left.
func (a *App) Quitting() bool { return a.quit }

// Screen is the picture as last drawn.
func (a *App) Screen() *term.Screen { return a.screen }

// Handle gives the IDE one event.
func (a *App) Handle(ev term.Event) {
	a.settleFocus()
	a.scrolled = ev.Kind == term.EvMouse && (ev.Button == term.WheelUp || ev.Button == term.WheelDown)
	switch {
	case ev.Kind == term.EvResize:
		a.resize(ev.X, ev.Y)
	case a.dialog != nil:
		a.dialogKey(ev)
	case ev.Kind == term.EvMouse:
		a.mouse(ev)
	case a.menu.Active:
		a.Do(a.menu.Key(ev))
	case ev.Kind == term.EvPaste:
		a.paste(ev.Text)
	default:
		a.key(ev)
	}
	a.sync()
}

// firstIntel is the language intelligence the IDE starts with: the one it
// was given, or the one made for the project it starts in — made here,
// after the survey, so that it is started knowing how the project stands
// with its go.work.
func (a *App) firstIntel() Intel {
	switch {
	case a.deps.Intel != nil:
		return a.deps.Intel
	case a.deps.Reroot != nil:
		return a.deps.Reroot(a.root)
	}
	return NoIntel()
}

// settleFocus takes the keyboard back to the text when the part that had
// it is not on the screen: a tree hidden by a narrow terminal, a bottom
// window zoomed away.
func (a *App) settleFocus() {
	at := a.layout()
	if (a.focus == focusTree && at.tree.W == 0) || (a.focus == focusPane && at.bottom.H == 0) {
		a.focus = focusText
	}
}

// resize takes a new terminal size: a new screen, and a whole frame next.
func (a *App) resize(w, h int) {
	a.screen, a.shown = term.NewScreen(w, h), nil
}

// dialogKey gives the dialog an event and, when it is answered, takes it
// down and says how.
func (a *App) dialogKey(ev term.Event) {
	if ev.Kind == term.EvMouse {
		return
	}
	outcome := a.dialog.Key(ev)
	if outcome == ui.Pending {
		return
	}
	then := a.answered
	a.dialog, a.answered = nil, nil
	if then != nil {
		then(outcome)
	}
}

// key gives a key to whoever has the keyboard: the completion list, a
// command bound to the key, the bottom window, or the text.
func (a *App) key(ev term.Event) {
	a.status = ""
	if a.popup != nil && a.popupKey(ev) {
		return
	}
	if a.focus == focusPane && a.paneKey(ev) {
		return
	}
	if a.focus == focusTree && a.treeKey(ev) {
		return
	}
	if name, ok := keymap[chordOf(ev)]; ok {
		a.Do(name)
		return
	}
	if ev.Mods == term.Alt && ev.Key == term.KeyRune && a.menu.Pull(ev.Rune) {
		a.popup = nil
		return
	}
	if ev.Key == term.KeyRune && ev.Mods == 0 {
		a.typed(ev.Rune)
	}
}

// chord is a key with the modifiers held.
type chord struct {
	key  term.KeyCode
	r    rune
	mods term.Mods
}

// chordOf is the chord of an event. A plain character is typing, not a
// chord, and answers the zero chord, which is bound to nothing.
func chordOf(ev term.Event) chord {
	if ev.Key != term.KeyRune {
		return chord{key: ev.Key, mods: ev.Mods}
	}
	if ev.Mods&(term.Ctrl|term.Alt) == 0 {
		return chord{}
	}
	return chord{key: term.KeyRune, r: unicode.ToLower(ev.Rune), mods: ev.Mods}
}

// Do runs a command by its name; a name that is not a command does
// nothing.
func (a *App) Do(name string) {
	if c, ok := commands[name]; ok {
		a.popup = nil
		c.Run(a)
	}
}

// say puts a message on the status line.
func (a *App) say(message string) { a.status = message }

// fail puts an error on the status line.
func (a *App) fail(err error) { a.status = oneLine(err.Error()) }
