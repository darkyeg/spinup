package agentconfig

// topLevel marks the lines that start outside any multi-line array or inline table.
func topLevel(lines []string) []bool {
	marks := make([]bool, len(lines))
	depth := 0
	for i, line := range lines {
		marks[i] = depth == 0
		if depth == 0 && tableHeader.MatchString(line) {
			continue
		}
		depth = max(0, depth+bracketBalance(line))
	}
	return marks
}

func bracketBalance(line string) int {
	balance := 0
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case quote != 0:
			switch {
			case escaped:
				escaped = false
			case r == 0x5c && quote == '"':
				escaped = true
			case r == quote:
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#':
			return balance
		case r == '[' || r == '{':
			balance++
		case r == ']' || r == '}':
			balance--
		}
	}
	return balance
}
