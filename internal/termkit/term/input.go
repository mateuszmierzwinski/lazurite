package term

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// What a terminal sends, turned into events.

// The bytes that open the sequences a terminal sends.
const (
	esc        = 0x1b
	pasteOpen  = "\x1b[200~"
	pasteClose = "\x1b[201~"
)

// Parse reads as many whole events as the bytes hold and returns what is
// left: the start of a sequence whose end has not arrived yet. final says no
// more bytes are coming for now, so a lone Escape is the Escape key and not
// the start of something.
func Parse(buf []byte, final bool) ([]Event, []byte) {
	var out []Event
	for len(buf) > 0 {
		ev, n := next(buf, final)
		if n == 0 {
			break
		}
		if ev.Kind != EvKey || ev.Key != KeyNone {
			out = append(out, ev)
		}
		buf = buf[n:]
	}
	return out, buf
}

// next reads one event from the front of the bytes and says how many it
// used; zero means the sequence there is not complete.
func next(buf []byte, final bool) (Event, int) {
	switch {
	case bytes.HasPrefix(buf, []byte(pasteOpen)):
		return paste(buf, final)
	case buf[0] == esc:
		return escape(buf, final)
	case buf[0] < 0x20 || buf[0] == 0x7f:
		return control(buf[0]), 1
	}
	r, n := utf8.DecodeRune(buf)
	if r == utf8.RuneError && n == 1 && !utf8.FullRune(buf) && !final {
		return Event{}, 0
	}
	return Event{Kind: EvKey, Key: KeyRune, Rune: r}, n
}

// control is the event of a control byte: Enter, Tab, Backspace, or Ctrl
// with a letter.
func control(b byte) Event {
	if ev, ok := controlKeys[b]; ok {
		return ev
	}
	return Event{Kind: EvKey, Key: KeyRune, Rune: rune(b) + 'a' - 1, Mods: Ctrl}
}

// controlKeys is the control bytes that are keys of their own rather than
// Ctrl with a letter.
var controlKeys = map[byte]Event{
	'\r': {Kind: EvKey, Key: KeyEnter},
	'\n': {Kind: EvKey, Key: KeyEnter},
	'\t': {Kind: EvKey, Key: KeyTab},
	0x7f: {Kind: EvKey, Key: KeyBackspace},
	0x08: {Kind: EvKey, Key: KeyBackspace},
	0x00: {Kind: EvKey, Key: KeyRune, Rune: ' ', Mods: Ctrl},
}

// paste reads a bracketed paste: everything between its two marks, as one
// event, cut at MaxPaste.
func paste(buf []byte, final bool) (Event, int) {
	body := buf[len(pasteOpen):]
	end := bytes.Index(body, []byte(pasteClose))
	if end < 0 {
		if !final && len(body) < MaxPaste {
			return Event{}, 0
		}
		end = len(body)
		return Event{Kind: EvPaste, Text: clip(body[:end])}, len(buf)
	}
	return Event{Kind: EvPaste, Text: clip(body[:end])}, len(pasteOpen) + end + len(pasteClose)
}

// clip is pasted bytes as text, no longer than MaxPaste, with the line
// endings of other systems made plain newlines.
func clip(b []byte) string {
	if len(b) > MaxPaste {
		b = b[:MaxPaste]
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// escape reads what follows an Escape byte: a control sequence, a function
// key, Alt with a key, or the Escape key itself.
func escape(buf []byte, final bool) (Event, int) {
	if len(buf) == 1 {
		if final {
			return Event{Kind: EvKey, Key: KeyEsc}, 1
		}
		return Event{}, 0
	}
	switch buf[1] {
	case '[':
		return csi(buf, final)
	case 'O':
		return ss3(buf, final)
	case esc:
		return Event{Kind: EvKey, Key: KeyEsc}, 1
	}
	ev, n := next(buf[1:], final)
	if n == 0 {
		return Event{}, 0
	}
	ev.Mods |= Alt
	return ev, n + 1
}

// ss3 reads the short form some terminals use for F1–F4 and the arrows.
func ss3(buf []byte, final bool) (Event, int) {
	if len(buf) < 3 {
		return incomplete(final, len(buf))
	}
	if key, ok := letterKeys[buf[2]]; ok {
		return Event{Kind: EvKey, Key: key}, 3
	}
	return Event{}, 3
}

// incomplete is the answer for a sequence cut short: wait for more, or —
// when no more is coming — drop what there is.
func incomplete(final bool, have int) (Event, int) {
	if final {
		return Event{}, have
	}
	return Event{}, 0
}

// letterKeys is the key each final letter of a sequence stands for.
var letterKeys = map[byte]KeyCode{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'H': KeyHome, 'F': KeyEnd,
	'P': KeyF1, 'Q': KeyF2, 'R': KeyF3, 'S': KeyF4, 'Z': KeyBackTab,
}

// tildeKeys is the key each number before a '~' stands for.
var tildeKeys = map[int]KeyCode{
	1: KeyHome, 2: KeyInsert, 3: KeyDelete, 4: KeyEnd, 5: KeyPgUp, 6: KeyPgDn, 7: KeyHome, 8: KeyEnd,
	11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5, 17: KeyF6, 18: KeyF7, 19: KeyF8,
	20: KeyF9, 21: KeyF10, 23: KeyF11, 24: KeyF12,
}

// maxSequence bounds a control sequence: one longer than this is not a key,
// and is dropped rather than waited for.
const maxSequence = 32

// consoleKeys is F1–F5 as the Linux console sends them: `ESC [ [ A` to
// `ESC [ [ E`, a form no other terminal uses.
var consoleKeys = map[byte]KeyCode{'A': KeyF1, 'B': KeyF2, 'C': KeyF3, 'D': KeyF4, 'E': KeyF5}

// console reads the Linux console's own form of F1–F5.
func console(buf []byte, final bool) (Event, int) {
	if len(buf) < 4 {
		return incomplete(final, len(buf))
	}
	return Event{Kind: EvKey, Key: consoleKeys[buf[3]]}, 4
}

// csi reads a control sequence: its numbers, then the letter that ends it.
func csi(buf []byte, final bool) (Event, int) {
	if len(buf) > 2 && buf[2] == '[' {
		return console(buf, final)
	}
	end := 2
	for end < len(buf) && (buf[end] < 0x40 || buf[end] > 0x7e) {
		end++
	}
	if end >= len(buf) {
		if len(buf) > maxSequence {
			return Event{}, len(buf)
		}
		return incomplete(final, len(buf))
	}
	body, last := string(buf[2:end]), buf[end]
	if strings.HasPrefix(body, "<") && (last == 'M' || last == 'm') {
		return mouse(body[1:], last == 'm'), end + 1
	}
	return keyOf(numbers(body), last), end + 1
}

// numbers is the numbers of a sequence, separated by semicolons; one that
// is missing or not a number is zero.
func numbers(body string) []int {
	parts := strings.Split(body, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err == nil && n >= 0 && n < 1<<16 {
			out[i] = n
		}
	}
	return out
}

// keyOf is the key a sequence's numbers and final letter stand for, with
// the modifiers its second number carries.
func keyOf(nums []int, last byte) Event {
	ev := Event{Kind: EvKey}
	if len(nums) > 1 && nums[1] > 0 {
		ev.Mods = Mods(nums[1]-1) & (Shift | Alt | Ctrl)
	}
	if last == '~' {
		ev.Key = tildeKeys[nums[0]]
		return ev
	}
	ev.Key = letterKeys[last]
	if ev.Key == KeyBackTab {
		ev.Mods |= Shift
	}
	return ev
}

// mouse reads a mouse report: what the button did, and where.
func mouse(body string, release bool) Event {
	nums := numbers(body)
	if len(nums) < 3 {
		return Event{}
	}
	ev := Event{Kind: EvMouse, X: nums[1] - 1, Y: nums[2] - 1, Button: buttonOf(nums[0], release)}
	if ev.X < 0 || ev.Y < 0 {
		return Event{}
	}
	return ev
}

// buttonOf is what a mouse report's first number says happened.
func buttonOf(code int, release bool) Button {
	switch {
	case code&64 != 0 && code&1 == 0:
		return WheelUp
	case code&64 != 0:
		return WheelDown
	case code&3 != 0:
		return MouseOther
	case release:
		return MouseRelease
	case code&32 != 0:
		return MouseDrag
	}
	return MouseLeft
}
