package ui

import (
	"unicode"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// barStart is the column the first menu title is drawn at.
const barStart = 2

// Draw paints the bar and, when one is pulled down, the menu under it.
func (m *MenuBar) Draw(s *term.Screen) {
	s.Fill(term.Rect{W: s.W, H: 1}, ' ', Bar)
	x := barStart
	for i, menu := range m.Menus {
		plain, hot := Bar, BarHot
		if m.Active && i == m.At {
			plain, hot = BarChosen, BarChosenHt
		}
		s.Put(x-1, 0, ' ', plain)
		n := Hot(s, x, 0, menu.Title, plain, hot)
		s.Put(x+n, 0, ' ', plain)
		x += n + 2
	}
	if m.Active && m.Open {
		m.drawMenu(s)
	}
}

// origin is the column a menu's title starts at.
func (m *MenuBar) origin(at int) int {
	x := barStart
	for i := 0; i < at && i < len(m.Menus); i++ {
		x += HotLen(m.Menus[i].Title) + 2
	}
	return x
}

// box is where the pulled-down menu is drawn.
func (m *MenuBar) box(s *term.Screen) term.Rect {
	menu := m.Menus[m.At]
	w := 0
	for _, it := range menu.Items {
		w = max(w, HotLen(it.Label)+len([]rune(it.Keys))+3)
	}
	r := term.Rect{X: m.origin(m.At) - 2, Y: 1, W: w + 4, H: len(menu.Items) + 2}
	r.X = max(min(r.X, s.W-r.W-2), 0)
	return r
}

// drawMenu paints the pulled-down menu, its shadow and its chosen row.
func (m *MenuBar) drawMenu(s *term.Screen) {
	r := m.box(s)
	Shadow(s, r)
	s.Box(r, Bar)
	for i, it := range m.Menus[m.At].Items {
		y := r.Y + 1 + i
		if it.Label == "" {
			rule(s, r, y)
			continue
		}
		plain, hot := Bar, BarHot
		if i == m.Row {
			plain, hot = BarChosen, BarChosenHt
			s.Fill(term.Rect{X: r.X + 1, Y: y, W: r.W - 2, H: 1}, ' ', plain)
		}
		Hot(s, r.X+2, y, it.Label, plain, hot)
		s.Text(r.X+r.W-2-len([]rune(it.Keys)), y, it.Keys, plain, r.W)
	}
}

// rule draws a separating line across a menu.
func rule(s *term.Screen, r term.Rect, y int) {
	s.Put(r.X, y, '├', Bar)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		s.Put(x, y, '─', Bar)
	}
	s.Put(r.X+r.W-1, y, '┤', Bar)
}

// Enter gives the bar the keyboard, on its first menu, nothing pulled down.
func (m *MenuBar) Enter() {
	m.Active, m.Open, m.Row = true, false, 0
}

// Leave takes the keyboard away from the bar.
func (m *MenuBar) Leave() {
	m.Active, m.Open = false, false
}

// Pull opens the menu whose title's letter was pressed with Alt, and says
// whether there is one.
func (m *MenuBar) Pull(letter rune) bool {
	for i, menu := range m.Menus {
		if HotKey(menu.Title) == unicode.ToLower(letter) {
			m.Active, m.Open, m.At, m.Row = true, true, i, 0
			return true
		}
	}
	return false
}

// Key gives the bar a key. It answers the command of an item that was
// chosen, or nothing.

// back closes the pulled-down menu, or leaves the bar when none is open.
func (m *MenuBar) back() {
	if m.Open {
		m.Open = false
		return
	}
	m.Leave()
}

// step moves the chosen row, skipping the separating rules, or pulls the
// menu down when it is not.
func (m *MenuBar) step(by int) {
	if !m.Open {
		m.Open, m.Row = true, 0
		return
	}
	items := m.Menus[m.At].Items
	for range items {
		m.Row = (m.Row + by + len(items)) % len(items)
		if items[m.Row].Label != "" {
			return
		}
	}
}

// choose takes the chosen row: its command, and the bar gives up the
// keyboard.
func (m *MenuBar) choose() string {
	if !m.Open {
		m.Open, m.Row = true, 0
		return ""
	}
	items := m.Menus[m.At].Items
	if m.Row >= len(items) {
		return ""
	}
	command := items[m.Row].Command
	m.Leave()
	return command
}

// letter picks by a letter: a menu on the closed bar, an item in an open
// menu.
func (m *MenuBar) letter(r rune) string {
	if !m.Open {
		m.Pull(r)
		return ""
	}
	for i, it := range m.Menus[m.At].Items {
		if it.Label != "" && HotKey(it.Label) == unicode.ToLower(r) {
			m.Row = i
			return m.choose()
		}
	}
	return ""
}

// Click gives the bar a mouse press. It answers the command of an item
// pressed, and whether the press was the bar's at all.
func (m *MenuBar) Click(s *term.Screen, x, y int) (string, bool) {
	if y == 0 {
		return "", m.clickBar(x)
	}
	if !m.Active || !m.Open {
		return "", false
	}
	r := m.box(s)
	row := y - r.Y - 1
	if x <= r.X || x >= r.X+r.W-1 || row < 0 || row >= len(m.Menus[m.At].Items) {
		m.Leave()
		return "", true
	}
	if m.Menus[m.At].Items[row].Label == "" {
		return "", true
	}
	m.Row = row
	return m.choose(), true
}

// clickBar pulls down the menu whose title was pressed.
func (m *MenuBar) clickBar(x int) bool {
	for i, menu := range m.Menus {
		from := m.origin(i) - 1
		if x >= from && x < from+HotLen(menu.Title)+2 {
			m.Active, m.Open, m.At, m.Row = true, true, i, 0
			return true
		}
	}
	m.Leave()
	return true
}
