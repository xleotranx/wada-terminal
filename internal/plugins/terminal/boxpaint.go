package terminal

// boxQuads is the geometry of a box-drawing character's arms: the rectangles
// of the cell, in fractions of it, that paint them. Every arm reaches the
// centre of the cell, so neighbouring cells join without a seam, and the line
// of a double arm is two thin lines.
func boxQuads(a arms, cellW, cellH float32) [][4]float32 {
	var out [][4]float32
	for _, arm := range []struct {
		weight uint8
		up     bool
	}{{a.Up, true}, {a.Down, false}} {
		if arm.weight == 0 {
			continue
		}
		for _, line := range lineRuns(arm.weight, cellW) {
			if arm.up {
				out = append(out, [4]float32{line.at - line.thick/2, 0, line.thick, .5 + line.thick/2})
			} else {
				out = append(out, [4]float32{line.at - line.thick/2, .5 - line.thick/2, line.thick, .5 + line.thick/2})
			}
		}
	}
	for _, arm := range []struct {
		weight uint8
		left   bool
	}{{a.Left, true}, {a.Right, false}} {
		if arm.weight == 0 {
			continue
		}
		for _, line := range lineRuns(arm.weight, cellH) {
			if arm.left {
				out = append(out, [4]float32{0, line.at - line.thick/2, .5 + line.thick/2, line.thick})
			} else {
				out = append(out, [4]float32{.5 - line.thick/2, line.at - line.thick/2, .5 + line.thick/2, line.thick})
			}
		}
	}
	return out
}

// run is one line of an arm: where it sits across the cell, in fractions, and
// how thick it is.
type run struct{ at, thick float32 }

// lineRuns is where an arm of a weight draws its line: one thin line for light,
// a thicker one for heavy, and two thin ones for double. size is the cell's
// width for a vertical arm and its height for a horizontal one.
func lineRuns(weight uint8, size float32) []run {
	if size <= 0 {
		return nil
	}
	switch weight {
	case armLight:
		return []run{{.5, 1 / size}}
	case armHeavy:
		return []run{{.5, 2 / size}}
	case armDouble:
		// Two thin lines a pixel apart either side of the centre.
		return []run{{.5 - 1.5/size, 1 / size}, {.5 + 1.5/size, 1 / size}}
	}
	return nil
}

// hasArms reports whether any arm of a character joins something.
func hasArms(a arms) bool {
	return a.Up != 0 || a.Down != 0 || a.Left != 0 || a.Right != 0
}
