package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestPickNamesReadsNumbersAndRanges(t *testing.T) {
	names := []string{"one", "two", "three", "four", "five"}
	for _, c := range []struct {
		answer string
		want   []string
	}{
		{"2", []string{"two"}},
		{"3,1", []string{"one", "three"}},
		{"2 4-5", []string{"two", "four", "five"}},
		{"1-3", []string{"one", "two", "three"}},
		{"2,2 2", []string{"two"}},
	} {
		got, err := pickNames(c.answer, names)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("pickNames(%q) = %v, %v; want %v", c.answer, got, err, c.want)
		}
	}
}

func TestPickNamesRefusesWhatIsNotOnTheList(t *testing.T) {
	names := []string{"one", "two"}
	for _, answer := range []string{"0", "3", "x", "2-1", "1-9", ""} {
		if _, err := pickNames(answer, names); err == nil {
			t.Errorf("pickNames(%q) should not pick anything", answer)
		}
	}
}

func TestAnEmptyAnswerCancelsInsteadOfFailing(t *testing.T) {
	if _, err := pickNames("  ", []string{"one"}); !errors.Is(err, errCancelled) {
		t.Fatalf("empty answer = %v; want cancelled", err)
	}
}

func TestAllPicksEverything(t *testing.T) {
	names := []string{"one", "two", "three"}
	for _, answer := range []string{"all", "ALL", " a "} {
		got, err := pickNames(answer, names)
		if err != nil || !reflect.DeepEqual(got, names) {
			t.Errorf("pickNames(%q) = %v, %v; want everything", answer, got, err)
		}
	}
}
