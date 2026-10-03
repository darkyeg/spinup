package tui

import (
	"fmt"
	"slices"
)

// Tone colours a row without the model knowing what the rows are.
type Tone int

const (
	Plain Tone = iota
	Good
	Warn
	Dim
)

type Row struct {
	// ID is what the caller gets back when the row is acted on.
	ID    string
	Cells []string
	Tone  Tone
}

// Action is a key that does something to the marked rows, or to the row under the cursor.
type Action struct {
	Key  rune
	Name string
	// Label is the hint shown in the footer.
	Label string
	// Confirm is a yes/no question asked first, with %d standing for how many rows are affected.
	Confirm string
	// Ask is a prompt for a line of text, which comes back in Intent.Input.
	Ask string
}

type Tab struct {
	Title   string
	Columns []string
	Rows    []Row
	Actions []Action
	// Empty is shown instead of rows when there are none.
	Empty string
}

// Intent is a request for the caller to act; the model never changes anything itself.
type Intent struct {
	Action string
	IDs    []string
	Input  string
}

type mode int

const (
	browsing mode = iota
	confirming
	asking
)

// reserved keys never name an action: they move, mark, switch tabs or quit.
const reserved = "jkhlgGAq"

type Model struct {
	Title string
	// Banner is a standing line under the tabs, for something to fix.
	Banner string
	Tabs   []Tab
	// Notice is what the last action said; it clears on the next key.
	Notice string

	tab     int
	cursor  []int
	top     []int
	picked  []map[string]bool
	mode    mode
	pending Action
	targets []string
	input   []rune
	width   int
	height  int
	done    bool
}

func NewModel(title string, tabs []Tab) *Model {
	m := &Model{Title: title, width: 80, height: 24}
	m.Replace(tabs, "")
	return m
}

// Replace swaps in fresh tabs after an action, keeping the cursor where it was as far as the rows allow.
func (m *Model) Replace(tabs []Tab, notice string) {
	m.Tabs = tabs
	m.Notice = notice
	m.mode = browsing
	m.cursor = fit(m.cursor, len(tabs))
	m.top = fit(m.top, len(tabs))
	picked := make([]map[string]bool, len(tabs))
	for i, tab := range tabs {
		picked[i] = map[string]bool{}
		if i < len(m.picked) {
			for _, row := range tab.Rows {
				if m.picked[i][row.ID] {
					picked[i][row.ID] = true
				}
			}
		}
		m.cursor[i] = min(m.cursor[i], max(len(tab.Rows)-1, 0))
	}
	m.picked = picked
	m.tab = min(m.tab, max(len(tabs)-1, 0))
	m.scroll()
}

func fit(old []int, n int) []int {
	out := make([]int, n)
	copy(out, old)
	return out
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.scroll()
}

func (m *Model) Done() bool { return m.done }

func (m *Model) current() Tab {
	if len(m.Tabs) == 0 {
		return Tab{}
	}
	return m.Tabs[m.tab]
}

// rowsVisible is how many rows fit between the header and the footer.
func (m *Model) rowsVisible() int { return max(m.height-chromeLines, 1) }

func (m *Model) scroll() {
	if len(m.top) == 0 {
		return
	}
	visible, cursor := m.rowsVisible(), m.cursor[m.tab]
	switch {
	case cursor < m.top[m.tab]:
		m.top[m.tab] = cursor
	case cursor >= m.top[m.tab]+visible:
		m.top[m.tab] = cursor - visible + 1
	}
}

// Update handles one key press. It returns an Intent when the caller should act, and whether it did.
func (m *Model) Update(key Key) (Intent, bool) {
	m.Notice = ""
	if key.Kind == CtrlC {
		m.done = true
		return Intent{}, false
	}
	switch m.mode {
	case confirming:
		return m.confirm(key)
	case asking:
		return m.ask(key)
	}
	return m.browse(key)
}

func (m *Model) browse(key Key) (Intent, bool) {
	tab := m.current()
	last := max(len(tab.Rows)-1, 0)
	switch key.Kind {
	case Up:
		m.move(-1)
	case Down:
		m.move(1)
	case PageUp:
		m.move(-m.rowsVisible())
	case PageDown:
		m.move(m.rowsVisible())
	case Home:
		m.moveTo(0)
	case End:
		m.moveTo(last)
	case TabKey, Right:
		m.switchTab(1)
	case Left:
		m.switchTab(-1)
	case Enter:
		if len(tab.Actions) > 0 {
			return m.act(tab.Actions[0])
		}
	case Esc:
		if len(m.picked[m.tab]) > 0 {
			m.picked[m.tab] = map[string]bool{}
		} else {
			m.done = true
		}
	case Rune:
		return m.letter(key.Rune)
	}
	return Intent{}, false
}

func (m *Model) letter(r rune) (Intent, bool) {
	switch r {
	case 'j':
		m.move(1)
	case 'k':
		m.move(-1)
	case 'l':
		m.switchTab(1)
	case 'h':
		m.switchTab(-1)
	case 'g':
		m.moveTo(0)
	case 'G':
		m.moveTo(max(len(m.current().Rows)-1, 0))
	case ' ':
		m.toggle()
	case 'A':
		m.toggleAll()
	case 'q':
		m.done = true
	default:
		for _, action := range m.current().Actions {
			if action.Key == r && !slices.Contains([]rune(reserved), r) {
				return m.act(action)
			}
		}
	}
	return Intent{}, false
}

func (m *Model) move(by int) { m.moveTo(m.cursor[m.tab] + by) }
func (m *Model) moveTo(to int) {
	if len(m.current().Rows) == 0 {
		return
	}
	m.cursor[m.tab] = min(max(to, 0), len(m.current().Rows)-1)
	m.scroll()
}

func (m *Model) switchTab(by int) {
	if len(m.Tabs) == 0 {
		return
	}
	m.tab = (m.tab + by + len(m.Tabs)) % len(m.Tabs)
	m.scroll()
}

func (m *Model) toggle() {
	rows := m.current().Rows
	if len(rows) == 0 {
		return
	}
	id := rows[m.cursor[m.tab]].ID
	if m.picked[m.tab][id] {
		delete(m.picked[m.tab], id)
	} else {
		m.picked[m.tab][id] = true
	}
	m.move(1)
}

func (m *Model) toggleAll() {
	rows := m.current().Rows
	if len(m.picked[m.tab]) == len(rows) {
		m.picked[m.tab] = map[string]bool{}
		return
	}
	for _, row := range rows {
		m.picked[m.tab][row.ID] = true
	}
}

// aimed are the rows an action applies to: the marked ones, or else the one under the cursor.
func (m *Model) aimed() []string {
	rows := m.current().Rows
	var ids []string
	for _, row := range rows {
		if m.picked[m.tab][row.ID] {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 && len(rows) > 0 {
		ids = []string{rows[m.cursor[m.tab]].ID}
	}
	return ids
}

func (m *Model) act(action Action) (Intent, bool) {
	ids := m.aimed()
	if len(ids) == 0 {
		m.Notice = "Nothing here to " + action.Label + "."
		return Intent{}, false
	}
	switch {
	case action.Confirm != "":
		m.mode, m.pending, m.targets = confirming, action, ids
		return Intent{}, false
	case action.Ask != "":
		m.mode, m.pending, m.targets, m.input = asking, action, ids, nil
		return Intent{}, false
	}
	return Intent{Action: action.Name, IDs: ids}, true
}

func (m *Model) confirm(key Key) (Intent, bool) {
	m.mode = browsing
	if key.Kind == Rune && (key.Rune == 'y' || key.Rune == 'Y') {
		return Intent{Action: m.pending.Name, IDs: m.targets}, true
	}
	m.Notice = "Cancelled."
	return Intent{}, false
}

func (m *Model) ask(key Key) (Intent, bool) {
	switch key.Kind {
	case Esc:
		m.mode, m.Notice = browsing, "Cancelled."
	case Enter:
		m.mode = browsing
		if len(m.input) == 0 {
			m.Notice = "Cancelled."
			return Intent{}, false
		}
		return Intent{Action: m.pending.Name, IDs: m.targets, Input: string(m.input)}, true
	case Backspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case Rune:
		m.input = append(m.input, key.Rune)
	}
	return Intent{}, false
}

// prompt is the line shown at the bottom while a question is open, or "" when browsing.
func (m *Model) prompt() string {
	switch m.mode {
	case confirming:
		return fmt.Sprintf(m.pending.Confirm, len(m.targets)) + " [y/N] "
	case asking:
		return m.pending.Ask + " " + string(m.input)
	}
	return ""
}
