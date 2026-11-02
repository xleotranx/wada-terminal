package vt

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// parserState is where the byte parser is in an escape sequence. The state is
// kept in the Screen, so a sequence may end at a Write boundary and continue
// in the next call.
type parserState uint8

const (
	stateGround parserState = iota
	stateEscape
	stateCSI
	stateOSC
	stateOSCEscape
	stateString // DCS, SOS, PM and APC: ignored until ST
	stateStringEscape
	stateCharset // ESC ( ) * + - . / # % $ &: consume and ignore
)

const maxOSCLength = 1 << 20

// parser is the part of the Screen that remembers an incomplete sequence.
type parser struct {
	state parserState

	csiParams        []byte
	csiIntermediates []byte
	csiPrivate       byte

	osc  []byte
	utf8 []byte
}

func (s *Screen) resetParser() {
	s.ps = parser{}
}

func (s *Screen) feed(b byte) {
	switch s.ps.state {
	case stateGround:
		s.groundByte(b)
	case stateEscape:
		s.escapeByte(b)
	case stateCSI:
		s.csiByte(b)
	case stateOSC:
		s.oscByte(b)
	case stateOSCEscape:
		s.oscEscapeByte(b)
	case stateString:
		if b == 0x1b {
			s.ps.state = stateStringEscape
		}
	case stateStringEscape:
		switch b {
		case '\\':
			s.ps.state = stateGround
		case 0x1b:
			// Stay in the escape half of a split ST.
		default:
			s.ps.state = stateString
		}
	case stateCharset:
		s.charsetByte(b)
	}
}

func (s *Screen) groundByte(b byte) {
	if b >= 0x80 {
		if len(s.ps.utf8) == 0 && b <= 0x9f {
			s.c1Byte(b)
			return
		}
		s.feedUTF8(b)
		return
	}
	s.ps.utf8 = s.ps.utf8[:0]
	switch {
	case b == 0x1b:
		s.ps.state = stateEscape
	case b < 0x20:
		s.control(b)
	case b == 0x7f:
		// DEL is ignored.
	default:
		s.putRune(rune(b))
	}
}

// c1Byte handles the 8-bit forms of CSI, OSC, DCS/APC/PM/SOS and ST.
func (s *Screen) c1Byte(b byte) {
	switch b {
	case 0x90: // DCS
		s.ps.state = stateString
	case 0x9b: // CSI
		s.resetCSI()
		s.ps.state = stateCSI
	case 0x9d: // OSC
		s.ps.osc = s.ps.osc[:0]
		s.ps.state = stateOSC
	case 0x9c: // ST, with nothing open
		s.ps.state = stateGround
	}
}

func (s *Screen) control(b byte) {
	switch b {
	case 0x07: // BEL
		s.bellFlag = true
	case 0x08: // BS
		s.backspace()
	case 0x09: // HT
		s.tabForward()
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		s.lineFeed()
	case 0x0d: // CR
		s.carriageReturn()
	case 0x0e, 0x0f: // SO, SI
		// The charset is ignored, so these select nothing.
	}
}

func (s *Screen) feedUTF8(b byte) {
	s.ps.utf8 = append(s.ps.utf8, b)
	for len(s.ps.utf8) > 0 {
		if !utf8.FullRune(s.ps.utf8) {
			return
		}
		r, size := utf8.DecodeRune(s.ps.utf8)
		if r == utf8.RuneError && size <= 1 {
			// Invalid byte: drop it and keep what follows.
			s.ps.utf8 = s.ps.utf8[1:]
			continue
		}
		s.ps.utf8 = s.ps.utf8[size:]
		s.putRune(r)
	}
}

// putRune writes one printable character. Zero-width code points join the
// previous cell; wide ones take two; a character at the right margin leaves a
// pending wrap that the next printable character resolves.
func (s *Screen) putRune(r rune) {
	if r == 0 {
		return
	}
	if isZeroWidth(r) {
		s.appendCombining(r)
		return
	}
	w := runeWidth(r)
	if w <= 0 {
		// A control-like code point is not a cell and has no glyph.
		return
	}
	if w > 2 {
		w = 2
	}

	if s.wrapNext {
		s.wrapNext = false
		if s.autoWrap {
			s.wrapFlags()[s.curY] = true
			s.curX = 0
			s.index()
		}
	}

	if w == 2 {
		if s.cols == 1 {
			w = 1
		} else if s.curX >= s.cols-1 {
			if !s.autoWrap {
				return
			}
			s.curX = 0
			s.index()
		}
	}

	x, y := s.curX, s.curY
	s.setCell(x, y, Cell{Text: string(r), Style: s.style, Width: uint8(w), Link: s.currentLink})
	s.lastX, s.lastY, s.lastValid = x, y, true
	s.curX += w
	if s.curX >= s.cols {
		s.curX = s.cols - 1
		if s.autoWrap {
			s.wrapNext = true
		}
	}
}

// appendCombining adds a mark to the cell of the previous character, leaving
// the cursor where it is.
func (s *Screen) appendCombining(r rune) {
	if !s.lastValid || s.lastY < 0 || s.lastY >= s.rows {
		return
	}
	line := s.gridLine(s.lastY)
	if s.lastX < 0 || s.lastX >= len(line) {
		return
	}
	x := s.lastX
	if line[x].Width == 0 {
		if x == 0 || line[x-1].Width != 2 {
			return
		}
		x--
		s.lastX = x
	}
	if line[x].Text == "" {
		return
	}
	line[x].Text += string(r)
}

// escapeByte handles the byte after ESC.
func (s *Screen) escapeByte(b byte) {
	switch b {
	case '[':
		s.resetCSI()
		s.ps.state = stateCSI
	case ']':
		s.ps.osc = s.ps.osc[:0]
		s.ps.state = stateOSC
	case 'P', '_', '^', 'X':
		// DCS, APC, PM and SOS: ignore everything until ST.
		s.ps.state = stateString
	case '(', ')', '*', '+', '-', '.', '/', '#', '%', '$', '&':
		s.ps.state = stateCharset
	case '7':
		s.saveCursor()
		s.ps.state = stateGround
	case '8':
		s.restoreCursor()
		s.ps.state = stateGround
	case 'D':
		s.index()
		s.lastValid = false
		s.ps.state = stateGround
	case 'E':
		s.nextLine()
		s.ps.state = stateGround
	case 'M':
		s.reverseIndex()
		s.ps.state = stateGround
	case 'H':
		if s.curX >= 0 && s.curX < len(s.tabs) {
			s.tabs[s.curX] = true
		}
		s.ps.state = stateGround
	case 'G':
		// Charset selection is ignored.
		s.ps.state = stateGround
	case 'c':
		s.fullReset()
	case '=', '>':
		// Keypad mode is ignored.
		s.ps.state = stateGround
	case '\\':
		// A stray string terminator.
		s.ps.state = stateGround
	case 0x1b:
		// ESC ESC: start the escape again.
	case 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d:
		s.control(b)
		s.ps.state = stateGround
	default:
		s.ps.state = stateGround
	}
}

// charsetByte consumes the one-byte designator of ESC ( etc.
func (s *Screen) charsetByte(b byte) {
	switch {
	case b == 0x1b:
		s.ps.state = stateEscape
	case b >= 0x20 && b <= 0x2f:
		// A second intermediate, as in ESC $ ) C.
	case b >= 0x30 && b <= 0x7e:
		s.ps.state = stateGround
	default:
		s.ps.state = stateGround
	}
}

func (s *Screen) resetCSI() {
	s.ps.csiParams = s.ps.csiParams[:0]
	s.ps.csiIntermediates = s.ps.csiIntermediates[:0]
	s.ps.csiPrivate = 0
}

func (s *Screen) csiByte(b byte) {
	switch {
	case b == 0x1b:
		s.ps.state = stateEscape
	case b >= 0x40 && b <= 0x7e:
		s.dispatchCSI(b)
		s.ps.state = stateGround
		s.resetCSI()
	case b >= 0x20 && b <= 0x2f:
		s.ps.csiIntermediates = append(s.ps.csiIntermediates, b)
	case b >= 0x30 && b <= 0x3f:
		if len(s.ps.csiParams) == 0 && len(s.ps.csiIntermediates) == 0 &&
			(b == '?' || b == '<' || b == '=' || b == '>') {
			s.ps.csiPrivate = b
			return
		}
		if b == ':' {
			// xterm accepts colon-separated colour subparameters; treating
			// them as semicolons is enough for SGR.
			b = ';'
		}
		s.ps.csiParams = append(s.ps.csiParams, b)
	}
}

func (s *Screen) dispatchCSI(final byte) {
	raw := string(s.ps.csiParams)
	params := parseCSIParams(raw)
	private := s.ps.csiPrivate
	intermediate := string(s.ps.csiIntermediates)

	switch final {
	case 'A': // CUU
		s.moveUp(csiPositive(params, 0, 1))
	case 'B': // CUD
		s.moveDown(csiPositive(params, 0, 1))
	case 'C': // CUF
		s.moveRight(csiPositive(params, 0, 1))
	case 'D': // CUB
		s.moveLeft(csiPositive(params, 0, 1))
	case 'E': // CNL
		s.nextLineCursor(csiPositive(params, 0, 1))
	case 'F': // CPL
		s.prevLineCursor(csiPositive(params, 0, 1))
	case 'G': // CHA
		s.hpa(csiPositive(params, 0, 1))
	case 'H', 'f': // CUP, HVP
		s.cup(csiPositive(params, 0, 1), csiPositive(params, 1, 1))
	case 'd': // VPA
		s.vpa(csiPositive(params, 0, 1))
	case '`': // HPA
		s.hpa(csiPositive(params, 0, 1))
	case 'J': // ED
		s.eraseDisplay(csiParam(params, 0, 0))
	case 'K': // EL
		s.eraseLine(csiParam(params, 0, 0))
	case '@': // ICH
		s.insertCells(csiPositive(params, 0, 1))
	case 'P': // DCH
		s.deleteCells(csiPositive(params, 0, 1))
	case 'X': // ECH
		s.eraseChars(csiPositive(params, 0, 1))
	case 'L': // IL
		s.insertLines(csiPositive(params, 0, 1))
	case 'M': // DL
		s.deleteLines(csiPositive(params, 0, 1))
	case 'S': // SU
		s.scrollUpLines(csiPositive(params, 0, 1))
	case 'T': // SD
		s.scrollDownLines(csiPositive(params, 0, 1))
	case 'r': // DECSTBM
		if private == 0 && intermediate == "" {
			s.setScrollRegion(params)
		}
	case 's': // save cursor
		if private == 0 {
			s.saveCursor()
		}
	case 'u': // restore cursor
		if private == 0 {
			s.restoreCursor()
		}
	case 'm': // SGR
		if private == 0 && intermediate == "" {
			s.sgr(raw)
		}
	case 'h', 'l': // DECSET / DECRST
		if private == '?' {
			for _, mode := range params {
				s.setPrivateMode(mode, final == 'h')
			}
		}
	case 'p':
		if intermediate == "!" {
			s.softReset()
		}
	case 'c': // DA: device attributes
		switch private {
		case '>':
			s.reply("\x1b[>0;95;0c") // a terminal that reports its version
		case 0:
			s.reply("\x1b[?62;22c") // VT220 with ANSI color
		}
	case 'n': // DSR: device status report
		switch {
		case private == '?' && csiParam(params, 0, 0) == 6: // DECXCPR
			x, y := s.reportCursor()
			s.reply("\x1b[?" + strconv.Itoa(y) + ";" + strconv.Itoa(x) + "R")
		case private == 0 && csiParam(params, 0, 0) == 5:
			s.reply("\x1b[0n")
		case private == 0 && csiParam(params, 0, 0) == 6: // CPR
			x, y := s.reportCursor()
			s.reply("\x1b[" + strconv.Itoa(y) + ";" + strconv.Itoa(x) + "R")
		}
	}
}

// reportCursor is the program's cursor as a report gives it: one-based, and
// inside the grid whatever the parser last saw.
func (s *Screen) reportCursor() (x, y int) {
	x = min(max(s.curX, 0), max(s.cols-1, 0)) + 1
	y = min(max(s.curY, 0), max(s.rows-1, 0)) + 1
	return x, y
}

func parseCSIParams(raw string) []int {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	params := make([]int, len(parts))
	for i, part := range parts {
		n := 0
		for j := 0; j < len(part); j++ {
			c := part[j]
			if c < '0' || c > '9' {
				n = 0
				break
			}
			if n < 1<<20 {
				n = n*10 + int(c-'0')
			}
		}
		params[i] = n
	}
	return params
}

func csiParam(params []int, i, def int) int {
	if i < 0 || i >= len(params) {
		return def
	}
	return params[i]
}

// csiPositive is a count parameter, where zero means the default too.
func csiPositive(params []int, i, def int) int {
	n := csiParam(params, i, def)
	if n <= 0 {
		return def
	}
	return n
}

func (s *Screen) oscByte(b byte) {
	switch b {
	case 0x07: // BEL terminates OSC
		s.finishOSC()
	case 0x1b:
		s.ps.state = stateOSCEscape
	default:
		if len(s.ps.osc) < maxOSCLength {
			s.ps.osc = append(s.ps.osc, b)
		}
	}
}

func (s *Screen) oscEscapeByte(b byte) {
	if b == '\\' {
		s.finishOSC()
		return
	}
	// An ESC not starting ST is part of the payload.
	if len(s.ps.osc)+1 < maxOSCLength {
		s.ps.osc = append(s.ps.osc, 0x1b, b)
	}
	s.ps.state = stateOSC
}

func (s *Screen) finishOSC() {
	payload := string(s.ps.osc)
	s.ps.state = stateGround
	s.ps.osc = s.ps.osc[:0]
	s.dispatchOSC(payload)
}

func (s *Screen) dispatchOSC(payload string) {
	command, data := payload, ""
	if i := strings.IndexByte(payload, ';'); i >= 0 {
		command, data = payload[:i], payload[i+1:]
	}
	switch command {
	case "0", "2":
		s.title = data
	case "7":
		s.cwd = osc7Path(data)
	case "8": // a hyperlink: OSC 8 ; params ; URI
		_, uri, _ := strings.Cut(data, ";")
		if uri == "" {
			s.currentLink = 0
		} else {
			s.currentLink = s.internLink(uri)
		}
	case "10": // the foreground color, which a program asks for
		if strings.TrimSpace(data) == "?" {
			s.reply("\x1b]10;" + rgbReply(s.fgRGB) + "\x1b\\")
		}
	case "11": // the background color
		if strings.TrimSpace(data) == "?" {
			s.reply("\x1b]11;" + rgbReply(s.bgRGB) + "\x1b\\")
		}
	case "7501": // the Program Status Protocol: what the program is doing
		if strings.TrimSpace(data) == "?" {
			s.reply(statusQueryReply)
			return
		}
		if st, ok := parseStatus(data); ok {
			s.applyStatus(st)
		}
	}
}

// rgbReply is a color as OSC 10 and 11 report it: three 16-bit components,
// the value of each byte repeated, as xterm prints them.
func rgbReply(c [3]uint8) string {
	return fmt.Sprintf("rgb:%04x/%04x/%04x", uint16(c[0])*257, uint16(c[1])*257, uint16(c[2])*257)
}

// osc7Path extracts the path from a file URL, which is what OSC 7 carries.
// url.Parse has already percent-decoded Path; the fallback decodes the part
// after the host by hand when the URL is not well formed.
func osc7Path(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme == "file" && u.Opaque == "" {
		return u.Path
	}
	rest, ok := strings.CutPrefix(raw, "file://")
	if !ok {
		return ""
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return ""
	}
	path := rest[slash:]
	if decoded, err := url.PathUnescape(path); err == nil {
		return decoded
	}
	return path
}
