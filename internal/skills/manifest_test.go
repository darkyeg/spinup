package skills

import (
	"reflect"
	"testing"
)

func mustParse(t *testing.T, text string) document {
	t.Helper()
	doc, err := parseDocument([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestMergeAddsPrivateSourcesAndManual(t *testing.T) {
	shared := mustParse(t, `{"agents":["claude-code"],"manual":["b"],"sources":{"o/a":["x"],"o/b":["y"]}}`)
	private := mustParse(t, `{"manual":["a","b"],"sources":{"o/a":["x","z"],"o/c":["w"]}}`)

	got := merge(shared, private)

	want := manifest{
		agents: []string{"claude-code"},
		manual: []string{"a", "b"},
		sources: []sourceEntry{
			{"o/a", []string{"x", "z"}}, {"o/b", []string{"y"}}, {"o/c", []string{"w"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRenderKeepsKeyOrderAndOneLinePerSource(t *testing.T) {
	doc := mustParse(t, `{
  "_comment": "keep <as is> & more",
  "agents": ["claude-code", "codex"],
  "extra": {"k": [1, 2]},
  "manual": [],
  "sources": {
    "o/a": [
      "x", "y"
    ],
    "o/empty": []
  }
}`)

	want := `{
  "_comment": "keep <as is> & more",
  "agents": ["claude-code", "codex"],
  "extra": {"k": [1, 2]},
  "manual": [],
  "sources": {
    "o/a": ["x", "y"]
  }
}
`
	if got := string(doc.render()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderOfMissingFileHasEmptySourcesAndManual(t *testing.T) {
	want := "{\n  \"sources\": {},\n  \"manual\": []\n}\n"
	if got := string(mustParse(t, "").render()); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestAddAppendsNewNamesAndMarksManual(t *testing.T) {
	doc := mustParse(t, `{"manual":["z"],"sources":{"o/a":["x"]}}`)

	doc.add("o/a", []string{"x", "y"}, Manual)
	doc.add("o/b", []string{"w"}, Auto)

	want := []sourceEntry{{"o/a", []string{"x", "y"}}, {"o/b", []string{"w"}}}
	if !reflect.DeepEqual(doc.sources, want) || !reflect.DeepEqual(doc.manual, []string{"x", "y", "z"}) {
		t.Fatalf("got %+v manual %v", doc.sources, doc.manual)
	}
}

func TestRemoveReportsWhetherAnythingWasListed(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		found bool
	}{
		{"listed", []string{"x"}, true},
		{"unknown", []string{"nope"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParse(t, `{"manual":["x"],"sources":{"o/a":["x","y"]}}`)
			if found := doc.remove(tt.names); found != tt.found {
				t.Fatalf("found = %v", found)
			}
		})
	}
}

func TestSetModeAutoRemovesFromManual(t *testing.T) {
	doc := mustParse(t, `{"manual":["a","b"],"sources":{}}`)
	doc.setMode([]string{"a"}, Auto)
	doc.setMode([]string{"c"}, Manual)
	if !reflect.DeepEqual(doc.manual, []string{"b", "c"}) {
		t.Fatalf("manual = %v", doc.manual)
	}
}
