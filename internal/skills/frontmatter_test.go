package skills

import "testing"

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		text string
		want frontmatter
	}{
		{"none", "# Title\ndescription: no", frontmatter{}},
		{"one line", "---\nname: x\ndescription: Does a thing\n---\nbody", frontmatter{"x", "Does a thing", false}},
		{"quoted", "---\ndescription: \"Quoted text\"\n---\n", frontmatter{"", "Quoted text", false}},
		{"folded", "---\ndescription: >\n  first line\n  second line\nname: x\n---\n", frontmatter{"x", "first line second line", false}},
		{"windows newlines", "---\r\ndescription: a\r\n  b\r\n---\r\n", frontmatter{"", "a b", false}},
		{"author manual", "---\ndescription: d\ndisable-model-invocation: true\n---\n", frontmatter{"", "d", true}},
		{"no description", "---\nname: x\n---\n", frontmatter{name: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFrontmatter(tt.text); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
