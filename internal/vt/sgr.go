package vt

import (
	"strconv"
	"strings"
)

// sgr applies a Select Graphic Rendition parameter string. Colon
// subparameters were already turned into semicolons by the CSI parser, so the
// loop only sees semicolon-separated values; empty values count as zero, as
// SGR requires.
func (s *Screen) sgr(raw string) {
	tokens := []string{"0"}
	if raw != "" {
		tokens = strings.Split(raw, ";")
	}

	for i := 0; i < len(tokens); i++ {
		n := sgrInt(tokens[i])
		switch {
		case n == 0:
			s.style = Style{}
		case n == 1:
			s.style.Bold = true
		case n == 2:
			s.style.Dim = true
		case n == 3:
			s.style.Italic = true
		case n == 4:
			s.style.Underline = true
		case n == 5:
			s.style.Blink = true
		case n == 7:
			s.style.Reverse = true
		case n == 9:
			s.style.Strike = true
		case n == 21, n == 22: // normal intensity
			s.style.Bold = false
			s.style.Dim = false
		case n == 23:
			s.style.Italic = false
		case n == 24:
			s.style.Underline = false
		case n == 25:
			s.style.Blink = false
		case n == 27:
			s.style.Reverse = false
		case n == 29:
			s.style.Strike = false
		case n >= 30 && n <= 37:
			s.style.FG = ANSI(uint8(n - 30))
		case n == 39:
			s.style.FG = Default
		case n >= 40 && n <= 47:
			s.style.BG = ANSI(uint8(n - 40))
		case n == 49:
			s.style.BG = Default
		case n >= 90 && n <= 97:
			s.style.FG = ANSI(uint8(n - 90 + 8))
		case n >= 100 && n <= 107:
			s.style.BG = ANSI(uint8(n - 100 + 8))
		case n == 38, n == 48:
			foreground := n == 38
			modeIndex := nextSGRValue(tokens, i+1)
			if modeIndex < 0 {
				i = len(tokens)
				continue
			}
			switch sgrInt(tokens[modeIndex]) {
			case 5: // 38;5;n / 48;5;n
				valueIndex := nextSGRValue(tokens, modeIndex+1)
				if valueIndex < 0 {
					i = len(tokens)
					continue
				}
				c := ANSI(uint8(clampInt(sgrInt(tokens[valueIndex]), 0, 255)))
				if foreground {
					s.style.FG = c
				} else {
					s.style.BG = c
				}
				i = valueIndex
			case 2: // 38;2;r;g;b / 48;2;r;g;b
				r, g, b, end, ok := sgrRGB(tokens, modeIndex+1)
				if !ok {
					i = len(tokens)
					continue
				}
				c := RGB(uint8(clampInt(r, 0, 255)), uint8(clampInt(g, 0, 255)), uint8(clampInt(b, 0, 255)))
				if foreground {
					s.style.FG = c
				} else {
					s.style.BG = c
				}
				i = end
			}
		}
	}
}

func sgrInt(token string) int {
	n, err := strconv.Atoi(token)
	if err != nil {
		return 0
	}
	return n
}

// nextSGRValue finds the next non-empty subparameter. A colon form such as
// 38:2::r:g:b leaves an empty colour-space slot, which is skipped.
func nextSGRValue(tokens []string, from int) int {
	for i := from; i < len(tokens); i++ {
		if tokens[i] != "" {
			return i
		}
	}
	return -1
}

// sgrRGB collects the next three non-empty values, so both 38;2;r;g;b and
// 38:2::r:g:b give the same colour. It returns the index of the last token it
// consumed.
func sgrRGB(tokens []string, from int) (r, g, b, end int, ok bool) {
	values := make([]int, 0, 3)
	i := from
	for ; i < len(tokens) && len(values) < 3; i++ {
		if tokens[i] == "" {
			continue
		}
		values = append(values, sgrInt(tokens[i]))
	}
	if len(values) != 3 {
		return 0, 0, 0, 0, false
	}
	return values[0], values[1], values[2], i - 1, true
}
