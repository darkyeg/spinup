package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// errCancelled ends a command the user backed out of, without an error message.
var errCancelled = errors.New("cancelled")

// pickNames turns an answer like "2 5-7", or "all", into the chosen names, in list order and without repeats.
func pickNames(answer string, names []string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return nil, errCancelled
	case "all", "a":
		return slices.Clone(names), nil
	}
	var picked []int
	for _, field := range strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' }) {
		from, to, err := pickRange(field, len(names))
		if err != nil {
			return nil, err
		}
		for i := from; i <= to; i++ {
			if !slices.Contains(picked, i) {
				picked = append(picked, i)
			}
		}
	}
	slices.Sort(picked)
	chosen := make([]string, 0, len(picked))
	for _, i := range picked {
		chosen = append(chosen, names[i-1])
	}
	return chosen, nil
}

// pickRange reads "5" as 5..5 and "5-7" as 5..7, within 1..count.
func pickRange(field string, count int) (from, to int, err error) {
	first, last, isRange := strings.Cut(field, "-")
	if from, err = pickNumber(first, count); err != nil {
		return 0, 0, err
	}
	if !isRange {
		return from, from, nil
	}
	if to, err = pickNumber(last, count); err != nil {
		return 0, 0, err
	}
	if to < from {
		return 0, 0, fmt.Errorf("%q counts backwards", field)
	}
	return from, to, nil
}

func pickNumber(text string, count int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 1 || n > count {
		return 0, fmt.Errorf("%q isn't one of the numbers 1-%d", strings.TrimSpace(text), count)
	}
	return n, nil
}
