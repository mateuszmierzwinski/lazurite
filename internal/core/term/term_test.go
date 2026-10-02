package term

import (
	"strings"
	"testing"
)

func keys(t *testing.T, in string) []Event {
	t.Helper()
	evs, rest := Parse([]byte(in), true)
	if len(rest) != 0 {
		t.Fatalf("%q left %q", in, rest)
	}
	return evs
}

func TestParseReadsPlainKeysAndControlKeys(t *testing.T) {
	evs := keys(t, "aé\r\t\x7f\x13\x00")
	want := []Event{
		{Kind: EvKey, Key: KeyRune, Rune: 'a'}, {Kind: EvKey, Key: KeyRune, Rune: 'é'},
		{Kind: EvKey, Key: KeyEnter}, {Kind: EvKey, Key: KeyTab}, {Kind: EvKey, Key: KeyBackspace},
		{Kind: EvKey, Key: KeyRune, Rune: 's', Mods: Ctrl}, {Kind: EvKey, Key: KeyRune, Rune: ' ', Mods: Ctrl},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	for i := range want {
		if evs[i] != want[i] {
			t.Errorf("event %d is %+v, want %+v", i, evs[i], want[i])
		}
	}
}

func TestParseReadsArrowsFunctionKeysAndModifiers(t *testing.T) {
	for in, want := range map[string]Event{
		"\x1b[A":     {Kind: EvKey, Key: KeyUp},
		"\x1b[1;2D":  {Kind: EvKey, Key: KeyLeft, Mods: Shift},
		"\x1b[1;5C":  {Kind: EvKey, Key: KeyRight, Mods: Ctrl},
		"\x1b[1;6H":  {Kind: EvKey, Key: KeyHome, Mods: Ctrl | Shift},
		"\x1b[3~":    {Kind: EvKey, Key: KeyDelete},
		"\x1b[5~":    {Kind: EvKey, Key: KeyPgUp},
		"\x1b[6;2~":  {Kind: EvKey, Key: KeyPgDn, Mods: Shift},
		"\x1bOP":     {Kind: EvKey, Key: KeyF1},
		"\x1b[15~":   {Kind: EvKey, Key: KeyF5},
		"\x1b[21~":   {Kind: EvKey, Key: KeyF10},
		"\x1b[18;2~": {Kind: EvKey, Key: KeyF7, Mods: Shift},
		"\x1b[Z":     {Kind: EvKey, Key: KeyBackTab, Mods: Shift},
		"\x1bf":      {Kind: EvKey, Key: KeyRune, Rune: 'f', Mods: Alt},
		"\x1b":       {Kind: EvKey, Key: KeyEsc},
		"\x1bOA":     {Kind: EvKey, Key: KeyUp},
		"\x1b[[B":    {Kind: EvKey, Key: KeyF2},
		"\x1b[20;5~": {Kind: EvKey, Key: KeyF9, Mods: Ctrl},
		"\x1b[1;3R":  {Kind: EvKey, Key: KeyF3, Mods: Alt},
		"\x1bx":      {Kind: EvKey, Key: KeyRune, Rune: 'x', Mods: Alt},
	} {
		evs := keys(t, in)
		if len(evs) != 1 || evs[0] != want {
			t.Errorf("%q gave %+v, want %+v", in, evs, want)
		}
	}
}

func TestParseReadsTheMouse(t *testing.T) {
	for in, want := range map[string]Event{
		"\x1b[<0;10;5M":  {Kind: EvMouse, X: 9, Y: 4, Button: MouseLeft},
		"\x1b[<0;10;5m":  {Kind: EvMouse, X: 9, Y: 4, Button: MouseRelease},
		"\x1b[<32;11;5M": {Kind: EvMouse, X: 10, Y: 4, Button: MouseDrag},
		"\x1b[<64;1;1M":  {Kind: EvMouse, Button: WheelUp},
		"\x1b[<65;1;1M":  {Kind: EvMouse, Button: WheelDown},
		"\x1b[<2;3;3M":   {Kind: EvMouse, X: 2, Y: 2, Button: MouseOther},
	} {
		evs := keys(t, in)
		if len(evs) != 1 || evs[0] != want {
			t.Errorf("%q gave %+v, want %+v", in, evs, want)
		}
	}
}

func TestParseReadsAPasteWholeAndCutsOneThatIsTooLong(t *testing.T) {
	evs := keys(t, "\x1b[200~line one\r\nline\x1b[Atwo\x1b[201~x")
	if len(evs) != 2 || evs[0].Kind != EvPaste || evs[0].Text != "line one\nline\x1b[Atwo" || evs[1].Rune != 'x' {
		t.Fatalf("got %+v", evs)
	}
	huge := "\x1b[200~" + strings.Repeat("a", MaxPaste+500)
	evs, rest := Parse([]byte(huge), false)
	if len(evs) != 1 || len(evs[0].Text) != MaxPaste || len(rest) != 0 {
		t.Fatalf("an endless paste gave %d events, %d bytes of text, %d left", len(evs), len(evs[0].Text), len(rest))
	}
}

func TestParseWaitsForTheRestOfASequenceUnlessNoMoreIsComing(t *testing.T) {
	for _, half := range []string{"\x1b", "\x1b[", "\x1b[1;", "\x1bO", "\x1b[200~abc", "\xc3", "\x1b[["} {
		evs, rest := Parse([]byte("a"+half), false)
		if len(evs) != 1 || string(rest) != half {
			t.Errorf("%q: %d events, %q left", half, len(evs), rest)
		}
	}
	evs, rest := Parse([]byte("\x1b["), true)
	if len(evs) != 0 || len(rest) != 0 {
		t.Fatalf("a final half-sequence gave %+v and left %q", evs, rest)
	}
}

func TestParseDropsWhatItDoesNotKnowAndNeverStalls(t *testing.T) {
	junk := "\x1b[999;999;999x\x1b[?25h\x1bOz\x1b[<1M\x1b[<0;0;0M" + "\x1b[" + strings.Repeat("9", 100) + "a"
	evs, rest := Parse([]byte(junk), true)
	if len(rest) != 0 {
		t.Fatalf("%d bytes left", len(rest))
	}
	for _, ev := range evs {
		if ev.Kind == EvKey && ev.Key == KeyNone {
			t.Fatalf("an empty event was reported: %+v", ev)
		}
	}
	long := []byte("\x1b[" + strings.Repeat("1;", 40))
	if evs, rest := Parse(long, false); len(rest) != 0 || len(evs) != 0 {
		t.Fatalf("an overlong sequence was waited for: %d left", len(rest))
	}
}

func TestAScreenIsHeldInsideTheLargestDrawn(t *testing.T) {
	s := NewScreen(1<<30, -5)
	if s.W != MaxWidth || s.H != 1 || len(s.Cells) != MaxWidth {
		t.Fatalf("got %d×%d", s.W, s.H)
	}
}

func TestDrawingOutsideTheScreenIsIgnored(t *testing.T) {
	s := NewScreen(10, 4)
	s.Put(-1, 0, 'x', Style{})
	s.Put(10, 0, 'x', Style{})
	s.Put(0, 4, 'x', Style{})
	s.Fill(Rect{X: -5, Y: -5, W: 100, H: 100}, '#', Style{Fg: White})
	if s.At(0, 0).R != '#' || s.At(9, 3).R != '#' || s.At(10, 0) != (Cell{}) {
		t.Fatal("a fill larger than the screen was not clipped to it")
	}
	if n := s.Text(8, 0, "abcdef", Style{}, 4); n != 4 || s.At(9, 0).R != 'b' {
		t.Fatalf("text used %d cells", n)
	}
}

func TestAControlCharacterNeverReachesTheScreen(t *testing.T) {
	s := NewScreen(10, 1)
	s.Text(0, 0, "a\x1b\x07\u009b\x7fb", Style{}, 10)
	out := string(Render(nil, s))
	if strings.Contains(out, "a\x1b") || strings.ContainsAny(out, "\x07\u009b\x7f") {
		t.Fatalf("a control character was sent to the terminal: %q", out)
	}
	if s.At(1, 0).R != ' ' || s.At(5, 0).R != 'b' {
		t.Fatalf("got %q %q", s.At(1, 0).R, s.At(5, 0).R)
	}
}

func TestBoxDrawsAFrameAndClearsItsInside(t *testing.T) {
	s := NewScreen(6, 4)
	s.Fill(Rect{W: 6, H: 4}, 'x', Style{})
	s.Box(Rect{X: 1, Y: 0, W: 4, H: 3}, Style{Fg: White})
	if s.At(1, 0).R != '┌' || s.At(4, 2).R != '┘' || s.At(2, 1).R != ' ' || s.At(0, 0).R != 'x' {
		t.Fatal("the box is wrong")
	}
	s.Box(Rect{W: 1, H: 1}, Style{})
}

func TestRenderSendsTheWholeScreenFirstAndOnlyWhatChangedAfter(t *testing.T) {
	a := NewScreen(20, 3)
	a.Text(0, 0, "hello", Style{Fg: Yellow, Bg: Blue}, 20)
	first := string(Render(nil, a))
	if !strings.Contains(first, "\x1b[2J") || !strings.Contains(first, "hello") || !strings.Contains(first, "\x1b[0;93;44m") {
		t.Fatalf("the first frame: %q", first)
	}
	b := NewScreen(20, 3)
	b.Text(0, 0, "hellp", Style{Fg: Yellow, Bg: Blue}, 20)
	second := string(Render(a, b))
	if strings.Contains(second, "\x1b[2J") || strings.Contains(second, "hell") || !strings.Contains(second, "\x1b[1;5H") || !strings.Contains(second, "p") {
		t.Fatalf("the second frame: %q", second)
	}
	if again := string(Render(b, b)); strings.Contains(again, "H") {
		t.Fatalf("an unchanged screen sent %q", again)
	}
}

func TestRenderShowsTheCursorOnlyWhereTheScreenPutsIt(t *testing.T) {
	s := NewScreen(10, 3)
	if strings.Contains(string(Render(nil, s)), "\x1b[?25h") {
		t.Fatal("a hidden cursor was shown")
	}
	s.CursorOn, s.CurX, s.CurY = true, 4, 2
	if out := string(Render(nil, s)); !strings.HasSuffix(out, "\x1b[3;5H\x1b[?25h") {
		t.Fatalf("got %q", out)
	}
	s.CurX = 99
	if strings.Contains(string(Render(nil, s)), "\x1b[?25h") {
		t.Fatal("a cursor outside the screen was shown")
	}
}

func TestSgrAndColourCodes(t *testing.T) {
	if fgCode(Blue) != 34 || fgCode(White) != 97 || fgCode(Yellow) != 93 {
		t.Fatal("fgCode")
	}
	if sgr(Style{Fg: Black, Bg: Grey, Underline: true}) != "\x1b[0;4;30;47m" {
		t.Fatalf("got %q", sgr(Style{Fg: Black, Bg: Grey, Underline: true}))
	}
}

func BenchmarkParse(b *testing.B) {
	in := []byte("abc\x1b[A\x1b[1;5C\x1b[<0;10;5Mxyz\x1b[15~")
	for b.Loop() {
		Parse(in, true)
	}
}

func BenchmarkRender(b *testing.B) {
	a, c := NewScreen(120, 40), NewScreen(120, 40)
	c.Text(10, 10, "func main() {", Style{Fg: Yellow, Bg: Blue}, 100)
	for b.Loop() {
		Render(a, c)
	}
}

func BenchmarkRenderFull(b *testing.B) {
	s := NewScreen(120, 40)
	for b.Loop() {
		Render(nil, s)
	}
}

func TestInvisibleCharactersThatReorderTextAreDrawnAsAMark(t *testing.T) {
	s := NewScreen(20, 1)
	s.Text(0, 0, string([]rune{0x61, 0x202e, 0x62, 0x2066, 0x63, 0x200b, 0x64, 0xfeff, 0x65, 0x200f, 0x66}), Style{}, 20)
	got := ""
	for x := 0; x < 11; x++ {
		got += string(s.At(x, 0).R)
	}
	if got != "a¿b¿c¿d¿e¿f" {
		t.Fatalf("got %q", got)
	}
	if hidden('a') || hidden('é') || hidden('─') {
		t.Fatal("an ordinary character is taken for a hidden one")
	}
}

func TestAfterAWideOrCombiningCharacterTheNextCellIsPlacedAgain(t *testing.T) {
	s := NewScreen(10, 1)
	s.Text(0, 0, "a漢b", Style{}, 10)
	out := string(Render(nil, s))
	if !strings.Contains(out, "a漢\x1b[1;3Hb") {
		t.Fatalf("got %q", out)
	}
	plain := NewScreen(10, 1)
	plain.Text(0, 0, "a║b", Style{}, 10)
	if strings.Contains(string(Render(nil, plain)), "║\x1b[1;3H") {
		t.Fatal("a box-drawing character was taken for a wide one")
	}
	if !uneven(rune(0x0301)) || !uneven(rune(0x1f600)) || uneven('x') || uneven('ż') || uneven('░') {
		t.Fatal("uneven")
	}
}

func TestTheClipboardSequenceCarriesOnlyEncodedTextAndIsBounded(t *testing.T) {
	seq := string(Clipboard("x\x07\x1b]0;title\x07"))
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\x07") || strings.Count(seq, "\x07") != 1 || strings.Count(seq, "\x1b") != 1 {
		t.Fatalf("the text broke out of the sequence: %q", seq)
	}
	if strings.Contains(seq, "?") {
		t.Fatal("the sequence asks the terminal for its clipboard")
	}
	if Clipboard("") != nil || Clipboard(strings.Repeat("x", maxClipboard+1)) != nil {
		t.Fatal("an empty or an oversized text was offered")
	}
}

func TestACloneIsNotChangedByLaterDrawing(t *testing.T) {
	s := NewScreen(4, 1)
	c := s.Clone()
	s.Put(0, 0, 'x', Style{})
	if c.At(0, 0).R == 'x' {
		t.Fatal("the clone shares the screen's cells")
	}
}

func BenchmarkPrintable(b *testing.B) {
	for b.Loop() {
		printable(rune(0x202e))
	}
}

func BenchmarkUneven(b *testing.B) {
	for b.Loop() {
		uneven('漢')
	}
}

func BenchmarkClipboard(b *testing.B) {
	for b.Loop() {
		Clipboard("some copied text")
	}
}
