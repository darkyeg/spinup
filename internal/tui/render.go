package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// chromeLines are the lines that are not rows: title, tabs, banner, column header, hints, notice.
const chromeLines = 6

const (
	reset   = "\x1b[0m"
	bold    = "\x1b[1m"
	dim     = "\x1b[2m"
	reverse = "\x1b[7m"
	green   = "\x1b[32m"
	yellow  = "\x1b[33m"
)

// View draws the whole screen as text a terminal can show, with each line cleared to its end so a
// shorter redraw leaves nothing behind.
func (m *Model) View(color bool) string {
	s := styler{color}
	lines := []string{
		s.wrap(bold, fit1(m.Title, m.width)),
		m.tabLine(s),
		s.wrap(yellow, fit1(m.Banner, m.width)),
		s.wrap(dim, fit1(m.header(), m.width)),
	}
	lines = append(lines, m.rowLines(s)...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines[:m.height-2], s.wrap(dim, fit1(m.hints(), m.width)), m.bottom(s))

	var out strings.Builder
	out.WriteString("\x1b[H")
	for i, line := range lines {
		if i > 0 {
			out.WriteString("\r\n")
		}
		out.WriteString(line + "\x1b[K")
	}
	return out.String()
}

type styler struct{ on bool }

func (s styler) wrap(style, text string) string {
	if !s.on || text == "" {
		return text
	}
	return style + text + reset
}

func (m *Model) tabLine(s styler) string {
	var parts []string
	for i, tab := range m.Tabs {
		if i == m.tab {
			parts = append(parts, s.wrap(bold+reverse, " "+tab.Title+" "))
		} else {
			parts = append(parts, " "+tab.Title+" ")
		}
	}
	return strings.Join(parts, " ")
}

func (m *Model) widths() []int {
	tab := m.current()
	widths := make([]int, len(tab.Columns))
	for i, name := range tab.Columns {
		widths[i] = utf8.RuneCountInString(name)
	}
	for _, row := range tab.Rows {
		for i, cell := range row.Cells {
			if i < len(widths) {
				widths[i] = max(widths[i], utf8.RuneCountInString(cell))
			}
		}
	}
	return widths
}

const gutter = "      " // the cursor mark and the checkbox

func (m *Model) header() string { return gutter + joinCells(m.current().Columns, m.widths()) }

func joinCells(cells []string, widths []int) string {
	var out []string
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		out = append(out, cell+strings.Repeat(" ", max(w-utf8.RuneCountInString(cell), 0)))
	}
	return strings.TrimRight(strings.Join(out, "  "), " ")
}

func (m *Model) rowLines(s styler) []string {
	tab := m.current()
	if len(tab.Rows) == 0 {
		return []string{"", "  " + fit1(tab.Empty, m.width-2)}
	}
	widths := m.widths()
	end := min(m.top[m.tab]+m.rowsVisible(), len(tab.Rows))
	var lines []string
	for i := m.top[m.tab]; i < end; i++ {
		row := tab.Rows[i]
		mark, box := "  ", "[ ] "
		if m.picked[m.tab][row.ID] {
			box = "[x] "
		}
		if i == m.cursor[m.tab] {
			mark = "> "
		}
		line := fit1(mark+box+joinCells(row.Cells, widths), m.width)
		lines = append(lines, s.wrap(m.style(row.Tone, i == m.cursor[m.tab]), line))
	}
	return lines
}

func (m *Model) style(tone Tone, cursor bool) string {
	style := map[Tone]string{Plain: "", Good: green, Warn: yellow, Dim: dim}[tone]
	if cursor {
		style += reverse
	}
	return style
}

// hints is the footer. On a narrow screen it drops the least useful hints first, and never the way out.
func (m *Model) hints() string {
	move, mark, all, next := "↑↓ move", "space mark", "A all", "tab next list"
	var actions []string
	for _, action := range m.current().Actions {
		actions = append(actions, fmt.Sprintf("%c %s", action.Key, action.Label))
	}
	quit := "q quit"
	for _, dropped := range [][]string{{}, {next}, {next, all}, {next, all, move}, {next, all, move, mark}} {
		var parts []string
		for _, part := range append(append([]string{move, mark, all}, actions...), next, quit) {
			if !slices.Contains(dropped, part) {
				parts = append(parts, part)
			}
		}
		if line := " " + strings.Join(parts, "  "); utf8.RuneCountInString(line) <= m.width {
			return line
		}
	}
	return " " + strings.Join(append(actions, quit), "  ")
}

func (m *Model) bottom(s styler) string {
	if prompt := m.prompt(); prompt != "" {
		return s.wrap(bold, fit1(" "+prompt, m.width))
	}
	return s.wrap(green, fit1(" "+m.Notice, m.width))
}

// fit1 cuts text to width runes, ending in an ellipsis when it had to cut.
func fit1(text string, width int) string {
	if width <= 0 || utf8.RuneCountInString(text) <= width {
		return text
	}
	return string([]rune(text)[:max(width-1, 0)]) + "…"
}
