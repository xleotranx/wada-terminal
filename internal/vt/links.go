package vt

// Hyperlink is one run of linked cells on the screen, with its URL and where
// it sits. Line counts from the oldest history line.
type Hyperlink struct {
	URL        string
	Line       int
	Start, End int
}

// Links lists the hyperlinks on the screen and in its history, in reading
// order. Runs that touch with the same URL are one link.
func (s *Screen) Links() []Hyperlink {
	var links []Hyperlink
	for line := 0; line < s.TotalLines(); line++ {
		cells := s.lineAt(line)
		var current *Hyperlink
		for x, cell := range cells {
			url := s.LinkOf(cell)
			if url == "" {
				if current != nil {
					links = append(links, *current)
					current = nil
				}
				continue
			}
			if current != nil && current.URL == url && current.End == x {
				current.End = x + 1
				continue
			}
			if current != nil {
				links = append(links, *current)
			}
			current = &Hyperlink{URL: url, Line: line, Start: x, End: x + 1}
		}
		if current != nil {
			links = append(links, *current)
		}
	}
	return links
}
