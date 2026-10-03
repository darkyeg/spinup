package tui

import (
	"reflect"
	"strings"
	"testing"
)

func press(m *Model, keys ...Key) (last Intent, acted bool) {
	for _, key := range keys {
		if intent, ok := m.Update(key); ok {
			last, acted = intent, true
		}
	}
	return last, acted
}

func letter(r rune) Key { return Key{Kind: Rune, Rune: r} }

func sample() *Model {
	return NewModel("skills", []Tab{
		{
			Title: "Yours", Columns: []string{"NAME", "MODE"},
			Rows: []Row{
				{ID: "alpha", Cells: []string{"alpha", "auto"}},
				{ID: "beta", Cells: []string{"beta", "manual"}},
				{ID: "gamma", Cells: []string{"gamma", "auto"}},
			},
			Actions: []Action{
				{Key: 'd', Name: "remove", Label: "remove", Confirm: "Remove %d skills?"},
				{Key: 'n', Name: "rename", Label: "rename", Ask: "New name:"},
				{Key: 'm', Name: "mode", Label: "manual/auto"},
			},
		},
		{Title: "Removed", Empty: "Nothing removed.", Actions: []Action{{Key: 'r', Name: "restore", Label: "restore"}}},
	})
}

func TestDecodeReadsArrowsKeysAndPastes(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []Key
	}{
		{"\x1b[A", []Key{{Kind: Up}}},
		{"\x1bOB", []Key{{Kind: Down}}},
		{"\x1b[6~", []Key{{Kind: PageDown}}},
		{"\x1b", []Key{{Kind: Esc}}},
		{"\r", []Key{{Kind: Enter}}},
		{"\t", []Key{{Kind: TabKey}}},
		{"\x7f", []Key{{Kind: Backspace}}},
		{"\x03", []Key{{Kind: CtrlC}}},
		{"jd", []Key{letter('j'), letter('d')}},
		{"é", []Key{letter('é')}},
		{"\x1b[Aq", []Key{{Kind: Up}, letter('q')}},
	} {
		if got := Decode([]byte(c.in)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Decode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestActionsApplyToTheRowUnderTheCursorUntilYouMarkSome(t *testing.T) {
	m := sample()
	intent, ok := press(m, letter('j'), letter('m'))
	if !ok || intent.Action != "mode" || !reflect.DeepEqual(intent.IDs, []string{"beta"}) {
		t.Fatalf("cursor action = %+v, %v", intent, ok)
	}

	m = sample()
	press(m, letter(' '), letter('j'), letter(' '))
	intent, ok = press(m, letter('m'))
	if !ok || !reflect.DeepEqual(intent.IDs, []string{"alpha", "gamma"}) {
		t.Fatalf("marked action = %+v, %v; marking moves down, so alpha and gamma", intent, ok)
	}
}

func TestADestructiveActionWaitsForYes(t *testing.T) {
	m := sample()
	if _, ok := press(m, letter('d')); ok {
		t.Fatal("remove acted before it was confirmed")
	}
	if !strings.Contains(m.View(false), "Remove 1 skills? [y/N]") {
		t.Fatalf("the question is not on screen:\n%s", m.View(false))
	}
	if _, ok := press(m, Key{Kind: Enter}); ok {
		t.Fatal("Enter confirmed a removal; only y may")
	}

	press(m, letter('d'))
	intent, ok := press(m, letter('y'))
	if !ok || intent.Action != "remove" || !reflect.DeepEqual(intent.IDs, []string{"alpha"}) {
		t.Fatalf("after y: %+v, %v", intent, ok)
	}
}

func TestAskCollectsATextAnswer(t *testing.T) {
	m := sample()
	press(m, letter('n'))
	press(m, letter('o'), letter('x'), Key{Kind: Backspace}, letter('k'))
	intent, ok := press(m, Key{Kind: Enter})
	if !ok || intent.Input != "ok" || intent.Action != "rename" {
		t.Fatalf("intent = %+v, %v", intent, ok)
	}

	press(m, letter('n'))
	if _, ok := press(m, Key{Kind: Enter}); ok {
		t.Fatal("an empty answer should cancel, not act")
	}
}

func TestMovingStaysInsideTheListAndTabsWrap(t *testing.T) {
	m := sample()
	press(m, Key{Kind: Up}, Key{Kind: Up})
	if m.cursor[0] != 0 {
		t.Fatalf("cursor went above the first row: %d", m.cursor[0])
	}
	press(m, Key{Kind: End}, Key{Kind: Down})
	if m.cursor[0] != 2 {
		t.Fatalf("cursor went below the last row: %d", m.cursor[0])
	}
	press(m, Key{Kind: TabKey}, Key{Kind: TabKey})
	if m.tab != 0 {
		t.Fatalf("tabs did not wrap: %d", m.tab)
	}
}

func TestAnActionOnAnEmptyListSaysSoInsteadOfActing(t *testing.T) {
	m := sample()
	press(m, Key{Kind: TabKey})
	if _, ok := press(m, letter('r')); ok {
		t.Fatal("restore acted with nothing to restore")
	}
	if !strings.Contains(m.Notice, "Nothing here") {
		t.Fatalf("notice = %q", m.Notice)
	}
}

func TestEscClearsMarksBeforeItQuits(t *testing.T) {
	m := sample()
	press(m, letter(' '), Key{Kind: Esc})
	if m.Done() || len(m.picked[0]) != 0 {
		t.Fatalf("first Esc should only clear the marks (done=%v, marks=%d)", m.Done(), len(m.picked[0]))
	}
	press(m, Key{Kind: Esc})
	if !m.Done() {
		t.Fatal("second Esc should quit")
	}
}

func TestReplaceKeepsTheCursorAndDropsMarksOfRowsThatLeft(t *testing.T) {
	m := sample()
	press(m, letter('j'), letter('j'), letter(' '))
	m.Replace([]Tab{{Title: "Yours", Columns: []string{"NAME"}, Rows: []Row{{ID: "alpha", Cells: []string{"alpha"}}}}, {Title: "Removed"}}, "done")
	if m.cursor[0] != 0 || len(m.picked[0]) != 0 || m.Notice != "done" {
		t.Fatalf("cursor %d, marks %v, notice %q", m.cursor[0], m.picked[0], m.Notice)
	}
}

func TestTheListScrollsToKeepTheCursorInView(t *testing.T) {
	var rows []Row
	for _, name := range strings.Fields("a b c d e f g h i j") {
		rows = append(rows, Row{ID: name, Cells: []string{name}})
	}
	m := NewModel("t", []Tab{{Title: "x", Columns: []string{"NAME"}, Rows: rows}})
	m.SetSize(40, chromeLines+3)
	press(m, Key{Kind: End})
	view := m.View(false)
	if !strings.Contains(view, "] j") || strings.Contains(view, "] a") {
		t.Fatalf("the last row should be visible and the first scrolled away:\n%s", view)
	}
}

func TestViewFillsTheScreenAndMarksTheCursorAndPicks(t *testing.T) {
	m := sample()
	m.SetSize(60, 12)
	m.Banner = "Two skills are both called review."
	press(m, letter(' '))
	lines := strings.Split(m.View(false), "\r\n")
	if len(lines) != 12 {
		t.Fatalf("drew %d lines for a 12-line screen", len(lines))
	}
	view := m.View(false)
	for _, want := range []string{"Yours", "Removed", "Two skills are both called review.", "[x] alpha", "> [ ] beta", "d remove", "q quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("screen lacks %q:\n%s", want, view)
		}
	}
}

func TestLongLinesAreCutToTheScreenWidth(t *testing.T) {
	m := NewModel("t", []Tab{{Title: "x", Columns: []string{"NAME"}, Rows: []Row{{ID: "a", Cells: []string{strings.Repeat("w", 200)}}}}})
	m.SetSize(30, 10)
	for _, line := range strings.Split(m.View(false), "\r\n") {
		plain := strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[H"), "\x1b[K")
		if n := len([]rune(plain)); n > 30 {
			t.Errorf("line is %d wide on a 30-wide screen: %q", n, plain)
		}
	}
}

func TestColourIsOffWhenAskedAndOnOtherwise(t *testing.T) {
	m := sample()
	if strings.Contains(m.View(false), "\x1b[7m") {
		t.Fatal("colour codes drawn with colour off")
	}
	if !strings.Contains(m.View(true), "\x1b[7m") {
		t.Fatal("the cursor row should be reversed with colour on")
	}
}

func TestTheFooterKeepsTheWayOutOnANarrowScreen(t *testing.T) {
	m := sample()
	for _, width := range []int{80, 60, 40, 30} {
		m.SetSize(width, 12)
		hints := m.hints()
		if !strings.Contains(hints, "q quit") || !strings.Contains(hints, "d remove") {
			t.Errorf("width %d lost an essential hint: %q", width, hints)
		}
	}
}
