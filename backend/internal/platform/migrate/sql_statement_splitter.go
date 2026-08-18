package migrate

import (
	"strings"
)

func splitSQLStatements(sql string) []string {
	statements := make([]string, 0, 8)
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	lineComment := false
	blockComment := false
	dollarQuoteTag := ""

	for index := 0; index < len(sql); index++ {
		char := sql[index]

		if lineComment {
			current.WriteByte(char)
			if char == '\n' {
				lineComment = false
			}
			continue
		}

		if blockComment {
			current.WriteByte(char)
			if char == '*' && index+1 < len(sql) && sql[index+1] == '/' {
				current.WriteByte(sql[index+1])
				index++
				blockComment = false
			}
			continue
		}

		if dollarQuoteTag != "" {
			if strings.HasPrefix(sql[index:], dollarQuoteTag) {
				current.WriteString(dollarQuoteTag)
				index += len(dollarQuoteTag) - 1
				dollarQuoteTag = ""
				continue
			}
			current.WriteByte(char)
			continue
		}

		if inSingleQuote {
			current.WriteByte(char)
			if char == '\'' {
				if index+1 < len(sql) && sql[index+1] == '\'' {
					current.WriteByte(sql[index+1])
					index++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		if inDoubleQuote {
			current.WriteByte(char)
			if char == '"' {
				if index+1 < len(sql) && sql[index+1] == '"' {
					current.WriteByte(sql[index+1])
					index++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		if char == '-' && index+1 < len(sql) && sql[index+1] == '-' {
			current.WriteByte(char)
			current.WriteByte(sql[index+1])
			index++
			lineComment = true
			continue
		}

		if char == '/' && index+1 < len(sql) && sql[index+1] == '*' {
			current.WriteByte(char)
			current.WriteByte(sql[index+1])
			index++
			blockComment = true
			continue
		}

		if char == '\'' {
			current.WriteByte(char)
			inSingleQuote = true
			continue
		}

		if char == '"' {
			current.WriteByte(char)
			inDoubleQuote = true
			continue
		}

		if char == '$' {
			if tag, ok := readDollarQuoteTag(sql[index:]); ok {
				current.WriteString(tag)
				index += len(tag) - 1
				dollarQuoteTag = tag
				continue
			}
		}

		if char == ';' {
			statement := strings.TrimSpace(current.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
			continue
		}

		current.WriteByte(char)
	}

	statement := strings.TrimSpace(current.String())
	if statement != "" {
		statements = append(statements, statement)
	}

	return statements
}

func readDollarQuoteTag(sql string) (string, bool) {
	if !strings.HasPrefix(sql, "$") {
		return "", false
	}
	for index := 1; index < len(sql); index++ {
		char := sql[index]
		if char == '$' {
			return sql[:index+1], true
		}
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return "", false
		}
	}
	return "", false
}
