package bw

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Pipes and files use a stable width. A terminal uses its current size.
func outputWidth(w io.Writer) int {
	if h, ok := w.(*humanWriter); ok {
		return h.width
	}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		if width, _, err := term.GetSize(f.Fd()); err == nil && width > 0 {
			return width
		}
	}
	return 100
}

// Human formatting never touches the JSON envelope or the interactive TUI.
// Buffer partial writes so wrapping does not depend on fmt's write boundaries.
type humanWriter struct {
	io.Writer
	width   int
	pending []byte
}

func (w *humanWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		if err := w.line(string(w.pending[:i])); err != nil {
			return 0, err
		}
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}
func (w *humanWriter) flush() {
	if len(w.pending) > 0 {
		_ = w.line(string(w.pending))
		w.pending = nil
	}
}
func displayText(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
func (w *humanWriter) line(s string) error {
	s = strings.ReplaceAll(displayText(s), "\t", "    ")
	if ansi.StringWidth(s) > w.width {
		leading := min(len(s)-len(strings.TrimLeft(s, " ")), max(0, w.width/4))
		indent := min(leading+2, max(0, w.width/4))
		// Keep the first-line prefix in place and indent only continuations.
		body := strings.TrimLeft(s, " ")
		pad := strings.Repeat(" ", indent)
		s = strings.Repeat(" ", leading) + strings.ReplaceAll(ansi.Wrap(body, max(2, w.width-indent), ""), "\n", "\n"+pad)
	}
	_, err := fmt.Fprintln(w.Writer, s)
	return err
}

// printTable wraps individual cells rather than letting a terminal wrap an
// oversized tabwriter row. Small terminals use labeled records instead.
func printTable(w io.Writer, headers []string, rows [][]string) {
	width := outputWidth(w)
	widths, floors := make([]int, len(headers)), make([]int, len(headers))
	for i, h := range headers {
		widths[i] = ansi.StringWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(clean(cell)))
		}
	}
	total, minimum := 2*(len(headers)-1), 2*(len(headers)-1)
	for i := range widths {
		floors[i] = max(ansi.StringWidth(headers[i]), min(16, widths[i]))
		total += widths[i]
		minimum += floors[i]
	}
	if minimum > width {
		for n, row := range rows {
			if n > 0 {
				fmt.Fprintln(w)
			}
			for i, cell := range row {
				fmt.Fprintf(w, "%s: %s\n", headers[i], clean(cell))
			}
		}
		return
	}
	for total > width {
		widest := -1
		for i := range widths {
			if widths[i] > floors[i] && (widest < 0 || widths[i] > widths[widest]) {
				widest = i
			}
		}
		widths[widest]--
		total--
	}
	printLine := func(cells []string) {
		for i, cell := range cells {
			fmt.Fprint(w, cell)
			if i < len(cells)-1 {
				fmt.Fprint(w, strings.Repeat(" ", max(0, widths[i]-ansi.StringWidth(cell))+2))
			}
		}
		fmt.Fprintln(w)
	}
	printLine(headers)
	for _, row := range rows {
		cells := make([][]string, len(headers))
		height := 1
		for i, cell := range row {
			cells[i] = strings.Split(ansi.Wrap(clean(cell), widths[i], ""), "\n")
			height = max(height, len(cells[i]))
		}
		for line := 0; line < height; line++ {
			values := make([]string, len(headers))
			for i := range cells {
				if line < len(cells[i]) {
					values[i] = cells[i][line]
				}
			}
			printLine(values)
		}
		if height > 1 {
			fmt.Fprintln(w)
		}
	}
}
