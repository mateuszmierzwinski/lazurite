package language

// Where a Ctrl+click goes.
//
// ONE rule, read twice. A jump is only a jump if it lands somewhere else:
//
//   - asked from a USE, the declaration is somewhere else, so the editor goes
//     there;
//   - asked from the DECLARATION, the server answers with the very range the
//     question was asked about — there is nowhere to go, and the question the
//     reader must have meant is "where is this used".
//
// The same filter answers the second half: a usage list that still holds the
// declaration would make a symbol used once look like a symbol used twice, and
// offer a list where the reader wanted to be taken somewhere.

// Elsewhere is the places that are not the one being asked about.
func Elsewhere(places []Location, path string, at Position) []Location {
	out := make([]Location, 0, len(places))
	for _, place := range places {
		if place.Path == path && place.Range.Touches(at) {
			continue
		}
		out = append(out, place)
	}
	return out
}

// Touches reports whether a position lies in a range or at either edge of it.
//
// Either EDGE, where the protocol's own reading of a range is half-open: a
// click at the end of a name is a click ON that name — the caret sits after the
// last letter and the server resolves the same symbol from it — so a rule that
// called that "somewhere else" would send the reader to where they already are.
func (r Range) Touches(at Position) bool {
	return !isBefore(at, r.Start) && !isBefore(r.End, at)
}

// isBefore reports whether one position comes before another in a file.
func isBefore(a, b Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}
