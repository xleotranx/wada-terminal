package terminal

// Arms are the four directions a box-drawing character can join a border
// in, each with the weight of its line: none, light, heavy or double.
type arms struct {
	Up, Down, Left, Right uint8
}

// The weights of an arm.
const (
	armNone   uint8 = 0
	armLight  uint8 = 1
	armHeavy  uint8 = 2
	armDouble uint8 = 3
)

// box joins up, down, left and right at their weights; armNone leaves a
// direction out. It keeps the table below to one line per character.
func box(up, down, left, right uint8) arms {
	return arms{Up: up, Down: down, Left: left, Right: right}
}

// horiz is a line from the left edge of the cell to its right edge.
func horiz(weight uint8) arms {
	return arms{Left: weight, Right: weight}
}

// vert is a line from the top edge of the cell to its bottom edge.
func vert(weight uint8) arms {
	return arms{Up: weight, Down: weight}
}

// boxDrawing is the arms of every box-drawing character the window draws:
// U+2500 through U+257F except the three diagonals. Each comment is the
// character's Unicode name, so an entry can be checked against the code chart
// without a lookup.
var boxDrawing = map[rune]arms{
	// Horizontal and vertical lines, solid and dashed.
	'─': horiz(armLight), // U+2500 LIGHT HORIZONTAL
	'━': horiz(armHeavy), // U+2501 HEAVY HORIZONTAL
	'│': vert(armLight),  // U+2502 LIGHT VERTICAL
	'┃': vert(armHeavy),  // U+2503 HEAVY VERTICAL
	'┄': horiz(armLight), // U+2504 LIGHT TRIPLE DASH HORIZONTAL
	'┅': horiz(armHeavy), // U+2505 HEAVY TRIPLE DASH HORIZONTAL
	'┆': vert(armLight),  // U+2506 LIGHT TRIPLE DASH VERTICAL
	'┇': vert(armHeavy),  // U+2507 HEAVY TRIPLE DASH VERTICAL
	'┈': horiz(armLight), // U+2508 LIGHT QUADRUPLE DASH HORIZONTAL
	'┉': horiz(armHeavy), // U+2509 HEAVY QUADRUPLE DASH HORIZONTAL
	'┊': vert(armLight),  // U+250A LIGHT QUADRUPLE DASH VERTICAL
	'┋': vert(armHeavy),  // U+250B HEAVY QUADRUPLE DASH VERTICAL

	// Light and heavy corners.
	'┌': box(armNone, armLight, armNone, armLight), // U+250C LIGHT DOWN AND RIGHT
	'┍': box(armNone, armLight, armNone, armHeavy), // U+250D DOWN LIGHT AND RIGHT HEAVY
	'┎': box(armNone, armHeavy, armNone, armLight), // U+250E DOWN HEAVY AND RIGHT LIGHT
	'┏': box(armNone, armHeavy, armNone, armHeavy), // U+250F HEAVY DOWN AND RIGHT
	'┐': box(armNone, armLight, armLight, armNone), // U+2510 LIGHT DOWN AND LEFT
	'┑': box(armNone, armLight, armHeavy, armNone), // U+2511 DOWN LIGHT AND LEFT HEAVY
	'┒': box(armNone, armHeavy, armLight, armNone), // U+2512 DOWN HEAVY AND LEFT LIGHT
	'┓': box(armNone, armHeavy, armHeavy, armNone), // U+2513 HEAVY DOWN AND LEFT
	'└': box(armLight, armNone, armNone, armLight), // U+2514 LIGHT UP AND RIGHT
	'┕': box(armLight, armNone, armNone, armHeavy), // U+2515 UP LIGHT AND RIGHT HEAVY
	'┖': box(armHeavy, armNone, armNone, armLight), // U+2516 UP HEAVY AND RIGHT LIGHT
	'┗': box(armHeavy, armNone, armNone, armHeavy), // U+2517 HEAVY UP AND RIGHT
	'┘': box(armLight, armNone, armLight, armNone), // U+2518 LIGHT UP AND LEFT
	'┙': box(armLight, armNone, armHeavy, armNone), // U+2519 UP LIGHT AND LEFT HEAVY
	'┚': box(armHeavy, armNone, armLight, armNone), // U+251A UP HEAVY AND LEFT LIGHT
	'┛': box(armHeavy, armNone, armHeavy, armNone), // U+251B HEAVY UP AND LEFT

	// Light and heavy tees.
	'├': box(armLight, armLight, armNone, armLight), // U+251C LIGHT VERTICAL AND RIGHT
	'┝': box(armLight, armLight, armNone, armHeavy), // U+251D VERTICAL LIGHT AND RIGHT HEAVY
	'┞': box(armHeavy, armLight, armNone, armLight), // U+251E UP HEAVY AND RIGHT DOWN LIGHT
	'┟': box(armLight, armHeavy, armNone, armLight), // U+251F DOWN HEAVY AND RIGHT UP LIGHT
	'┠': box(armHeavy, armHeavy, armNone, armLight), // U+2520 VERTICAL HEAVY AND RIGHT LIGHT
	'┡': box(armHeavy, armLight, armNone, armHeavy), // U+2521 DOWN LIGHT AND RIGHT UP HEAVY
	'┢': box(armLight, armHeavy, armNone, armHeavy), // U+2522 UP LIGHT AND RIGHT DOWN HEAVY
	'┣': box(armHeavy, armHeavy, armNone, armHeavy), // U+2523 HEAVY VERTICAL AND RIGHT
	'┤': box(armLight, armLight, armLight, armNone), // U+2524 LIGHT VERTICAL AND LEFT
	'┥': box(armLight, armLight, armHeavy, armNone), // U+2525 VERTICAL LIGHT AND LEFT HEAVY
	'┦': box(armHeavy, armLight, armLight, armNone), // U+2526 UP HEAVY AND LEFT DOWN LIGHT
	'┧': box(armLight, armHeavy, armLight, armNone), // U+2527 DOWN HEAVY AND LEFT UP LIGHT
	'┨': box(armHeavy, armHeavy, armLight, armNone), // U+2528 VERTICAL HEAVY AND LEFT LIGHT
	'┩': box(armHeavy, armLight, armHeavy, armNone), // U+2529 DOWN LIGHT AND LEFT UP HEAVY
	'┪': box(armLight, armHeavy, armHeavy, armNone), // U+252A UP LIGHT AND LEFT DOWN HEAVY
	'┫': box(armHeavy, armHeavy, armHeavy, armNone), // U+252B HEAVY VERTICAL AND LEFT
	'┬': box(armNone, armLight, armLight, armLight), // U+252C LIGHT DOWN AND HORIZONTAL
	'┭': box(armNone, armLight, armHeavy, armLight), // U+252D LEFT HEAVY AND RIGHT DOWN LIGHT
	'┮': box(armNone, armLight, armLight, armHeavy), // U+252E RIGHT HEAVY AND LEFT DOWN LIGHT
	'┯': box(armNone, armLight, armHeavy, armHeavy), // U+252F DOWN LIGHT AND HORIZONTAL HEAVY
	'┰': box(armNone, armHeavy, armLight, armLight), // U+2530 DOWN HEAVY AND HORIZONTAL LIGHT
	'┱': box(armNone, armHeavy, armHeavy, armLight), // U+2531 RIGHT LIGHT AND LEFT DOWN HEAVY
	'┲': box(armNone, armHeavy, armLight, armHeavy), // U+2532 LEFT LIGHT AND RIGHT DOWN HEAVY
	'┳': box(armNone, armHeavy, armHeavy, armHeavy), // U+2533 HEAVY DOWN AND HORIZONTAL
	'┴': box(armLight, armNone, armLight, armLight), // U+2534 LIGHT UP AND HORIZONTAL
	'┵': box(armLight, armNone, armHeavy, armLight), // U+2535 LEFT HEAVY AND RIGHT UP LIGHT
	'┶': box(armLight, armNone, armLight, armHeavy), // U+2536 RIGHT HEAVY AND LEFT UP LIGHT
	'┷': box(armLight, armNone, armHeavy, armHeavy), // U+2537 UP LIGHT AND HORIZONTAL HEAVY
	'┸': box(armHeavy, armNone, armLight, armLight), // U+2538 UP HEAVY AND HORIZONTAL LIGHT
	'┹': box(armHeavy, armNone, armHeavy, armLight), // U+2539 RIGHT LIGHT AND LEFT UP HEAVY
	'┺': box(armHeavy, armNone, armLight, armHeavy), // U+253A LEFT LIGHT AND RIGHT UP HEAVY
	'┻': box(armHeavy, armNone, armHeavy, armHeavy), // U+253B HEAVY UP AND HORIZONTAL

	// Light and heavy crosses. U+2543 through U+2546 are four-armed crosses:
	// the name's two direction pairs are the heavy and the light arms, not a
	// corner's two arms.
	'┼': box(armLight, armLight, armLight, armLight), // U+253C LIGHT VERTICAL AND HORIZONTAL
	'┽': box(armLight, armLight, armHeavy, armLight), // U+253D LEFT HEAVY AND RIGHT VERTICAL LIGHT
	'┾': box(armLight, armLight, armLight, armHeavy), // U+253E RIGHT HEAVY AND LEFT VERTICAL LIGHT
	'┿': box(armLight, armLight, armHeavy, armHeavy), // U+253F VERTICAL LIGHT AND HORIZONTAL HEAVY
	'╀': box(armHeavy, armLight, armLight, armLight), // U+2540 UP HEAVY AND DOWN HORIZONTAL LIGHT
	'╁': box(armLight, armHeavy, armLight, armLight), // U+2541 DOWN HEAVY AND UP HORIZONTAL LIGHT
	'╂': box(armHeavy, armHeavy, armLight, armLight), // U+2542 VERTICAL HEAVY AND HORIZONTAL LIGHT
	'╃': box(armHeavy, armLight, armHeavy, armLight), // U+2543 LEFT UP HEAVY AND RIGHT DOWN LIGHT
	'╄': box(armHeavy, armLight, armLight, armHeavy), // U+2544 RIGHT UP HEAVY AND LEFT DOWN LIGHT
	'╅': box(armLight, armHeavy, armHeavy, armLight), // U+2545 LEFT DOWN HEAVY AND RIGHT UP LIGHT
	'╆': box(armLight, armHeavy, armLight, armHeavy), // U+2546 RIGHT DOWN HEAVY AND LEFT UP LIGHT
	'╇': box(armHeavy, armLight, armHeavy, armHeavy), // U+2547 DOWN LIGHT AND UP HORIZONTAL HEAVY
	'╈': box(armLight, armHeavy, armHeavy, armHeavy), // U+2548 UP LIGHT AND DOWN HORIZONTAL HEAVY
	'╉': box(armHeavy, armHeavy, armHeavy, armLight), // U+2549 RIGHT LIGHT AND LEFT VERTICAL HEAVY
	'╊': box(armHeavy, armHeavy, armLight, armHeavy), // U+254A LEFT LIGHT AND RIGHT VERTICAL HEAVY
	'╋': box(armHeavy, armHeavy, armHeavy, armHeavy), // U+254B HEAVY VERTICAL AND HORIZONTAL

	// Light and heavy double-dash lines.
	'╌': horiz(armLight), // U+254C LIGHT DOUBLE DASH HORIZONTAL
	'╍': horiz(armHeavy), // U+254D HEAVY DOUBLE DASH HORIZONTAL
	'╎': vert(armLight),  // U+254E LIGHT DOUBLE DASH VERTICAL
	'╏': vert(armHeavy),  // U+254F HEAVY DOUBLE DASH VERTICAL

	// Double lines, and single and double line components. In the names
	// "single" is the light weight.
	'═': horiz(armDouble),                                // U+2550 DOUBLE HORIZONTAL
	'║': vert(armDouble),                                 // U+2551 DOUBLE VERTICAL
	'╒': box(armNone, armLight, armNone, armDouble),      // U+2552 DOWN SINGLE AND RIGHT DOUBLE
	'╓': box(armNone, armDouble, armNone, armLight),      // U+2553 DOWN DOUBLE AND RIGHT SINGLE
	'╔': box(armNone, armDouble, armNone, armDouble),     // U+2554 DOUBLE DOWN AND RIGHT
	'╕': box(armNone, armLight, armDouble, armNone),      // U+2555 DOWN SINGLE AND LEFT DOUBLE
	'╖': box(armNone, armDouble, armLight, armNone),      // U+2556 DOWN DOUBLE AND LEFT SINGLE
	'╗': box(armNone, armDouble, armDouble, armNone),     // U+2557 DOUBLE DOWN AND LEFT
	'╘': box(armLight, armNone, armNone, armDouble),      // U+2558 UP SINGLE AND RIGHT DOUBLE
	'╙': box(armDouble, armNone, armNone, armLight),      // U+2559 UP DOUBLE AND RIGHT SINGLE
	'╚': box(armDouble, armNone, armNone, armDouble),     // U+255A DOUBLE UP AND RIGHT
	'╛': box(armLight, armNone, armDouble, armNone),      // U+255B UP SINGLE AND LEFT DOUBLE
	'╜': box(armDouble, armNone, armLight, armNone),      // U+255C UP DOUBLE AND LEFT SINGLE
	'╝': box(armDouble, armNone, armDouble, armNone),     // U+255D DOUBLE UP AND LEFT
	'╞': box(armLight, armLight, armNone, armDouble),     // U+255E VERTICAL SINGLE AND RIGHT DOUBLE
	'╟': box(armDouble, armDouble, armNone, armLight),    // U+255F VERTICAL DOUBLE AND RIGHT SINGLE
	'╠': box(armDouble, armDouble, armNone, armDouble),   // U+2560 DOUBLE VERTICAL AND RIGHT
	'╡': box(armLight, armLight, armDouble, armNone),     // U+2561 VERTICAL SINGLE AND LEFT DOUBLE
	'╢': box(armDouble, armDouble, armLight, armNone),    // U+2562 VERTICAL DOUBLE AND LEFT SINGLE
	'╣': box(armDouble, armDouble, armDouble, armNone),   // U+2563 DOUBLE VERTICAL AND LEFT
	'╤': box(armNone, armLight, armDouble, armDouble),    // U+2564 DOWN SINGLE AND HORIZONTAL DOUBLE
	'╥': box(armNone, armDouble, armLight, armLight),     // U+2565 DOWN DOUBLE AND HORIZONTAL SINGLE
	'╦': box(armNone, armDouble, armDouble, armDouble),   // U+2566 DOUBLE DOWN AND HORIZONTAL
	'╧': box(armLight, armNone, armDouble, armDouble),    // U+2567 UP SINGLE AND HORIZONTAL DOUBLE
	'╨': box(armDouble, armNone, armLight, armLight),     // U+2568 UP DOUBLE AND HORIZONTAL SINGLE
	'╩': box(armDouble, armNone, armDouble, armDouble),   // U+2569 DOUBLE UP AND HORIZONTAL
	'╪': box(armLight, armLight, armDouble, armDouble),   // U+256A VERTICAL SINGLE AND HORIZONTAL DOUBLE
	'╫': box(armDouble, armDouble, armLight, armLight),   // U+256B VERTICAL DOUBLE AND HORIZONTAL SINGLE
	'╬': box(armDouble, armDouble, armDouble, armDouble), // U+256C DOUBLE VERTICAL AND HORIZONTAL

	// Rounded corners: the same arms as the light square corners.
	'╭': box(armNone, armLight, armNone, armLight), // U+256D LIGHT ARC DOWN AND RIGHT
	'╮': box(armNone, armLight, armLight, armNone), // U+256E LIGHT ARC DOWN AND LEFT
	'╯': box(armLight, armNone, armLight, armNone), // U+256F LIGHT ARC UP AND LEFT
	'╰': box(armLight, armNone, armNone, armLight), // U+2570 LIGHT ARC UP AND RIGHT

	// U+2571 through U+2573 are the diagonals: the window leaves them to the
	// font, so they are absent from this table.

	// Half lines.
	'╴': box(armNone, armNone, armLight, armNone),  // U+2574 LIGHT LEFT
	'╵': box(armLight, armNone, armNone, armNone),  // U+2575 LIGHT UP
	'╶': box(armNone, armNone, armNone, armLight),  // U+2576 LIGHT RIGHT
	'╷': box(armNone, armLight, armNone, armNone),  // U+2577 LIGHT DOWN
	'╸': box(armNone, armNone, armHeavy, armNone),  // U+2578 HEAVY LEFT
	'╹': box(armHeavy, armNone, armNone, armNone),  // U+2579 HEAVY UP
	'╺': box(armNone, armNone, armNone, armHeavy),  // U+257A HEAVY RIGHT
	'╻': box(armNone, armHeavy, armNone, armNone),  // U+257B HEAVY DOWN
	'╼': box(armNone, armNone, armLight, armHeavy), // U+257C LIGHT LEFT AND HEAVY RIGHT
	'╽': box(armLight, armHeavy, armNone, armNone), // U+257D LIGHT UP AND HEAVY DOWN
	'╾': box(armNone, armNone, armHeavy, armLight), // U+257E HEAVY LEFT AND LIGHT RIGHT
	'╿': box(armHeavy, armLight, armNone, armNone), // U+257F HEAVY UP AND LIGHT DOWN
}

// boxArms returns the arms of a box-drawing character, and whether r is one
// the window draws itself rather than leaving to the font.
func boxArms(r rune) (arms, bool) {
	if r < 0x2500 || r > 0x257F || (r >= '╱' && r <= '╳') {
		return arms{}, false
	}
	a, ok := boxDrawing[r]
	return a, ok
}
