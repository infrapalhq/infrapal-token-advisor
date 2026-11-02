package mcpserver

import (
	"fmt"
	"strings"
)

// contentColumns are columns that hold prompts, responses, commands, paths, or
// user identity. A view exposing any of them is withheld from the model.
var contentColumns = map[string]bool{
	"attrs":             true,
	"body":              true,
	"error":             true,
	"file_path":         true,
	"full_command":      true,
	"org_id":            true,
	"project_dir":       true,
	"prompt_text":       true,
	"request_body_ref":  true,
	"resource_attrs":    true,
	"response_body_ref": true,
	"response_text":     true,
	"tool_input":        true,
	"user_email":        true,
	"user_id":           true,
}

// builtinTables are SQLite's table-valued built-ins that can read outside the
// views and don't carry a sqlite_ or pragma_ prefix.
var builtinTables = map[string]bool{"bytecode": true, "dbstat": true, "tables_used": true}

// checkQuery allows one SELECT or WITH statement that mentions no blocked name.
//
// The driver has no authorizer hook, so this walks SQLite's tokens instead:
// comments are skipped and every identifier is compared unquoted. String
// literals are compared too, since SQLite accepts FROM 'table'. A column alias
// that matches a table is rejected as well; the error says to rename it.
func checkQuery(query string, blocked map[string]bool) error {
	if strings.IndexByte(query, 0) >= 0 {
		return fmt.Errorf("query contains a NUL byte")
	}
	check := func(name string) error {
		lower := strings.ToLower(name)
		if blocked[lower] || builtinTables[lower] || strings.HasPrefix(lower, "sqlite_") || strings.HasPrefix(lower, "pragma_") {
			return fmt.Errorf("%q is not available: use the views listed by get_schema, and rename it if it is an alias", name)
		}
		return nil
	}

	first, ended := true, false
	for i := 0; i < len(query); {
		c := query[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			i++
			continue
		case strings.HasPrefix(query[i:], "--"):
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				i = len(query)
				continue
			}
			i += end + 1
			continue
		case strings.HasPrefix(query[i:], "/*"):
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				i = len(query)
				continue
			}
			i += end + 4
			continue
		}

		if ended {
			return fmt.Errorf("only one statement is allowed")
		}
		if c == ';' {
			if first {
				return fmt.Errorf("only SELECT or WITH queries are allowed")
			}
			ended = true
			i++
			continue
		}

		token, next, quoted, err := nextToken(query, i)
		if err != nil {
			return err
		}
		if first {
			word := strings.ToUpper(token)
			if quoted || (word != "SELECT" && word != "WITH") {
				return fmt.Errorf("only SELECT or WITH queries are allowed")
			}
			first = false
		}
		if err := check(token); err != nil {
			return err
		}
		i = next
	}
	if first {
		return fmt.Errorf("query is empty")
	}
	return nil
}

// nextToken reads the token at query[i]: its unquoted text, the index after it,
// and whether it was quoted. Operators and punctuation come back one byte at a
// time and match no name.
func nextToken(query string, i int) (token string, next int, quoted bool, err error) {
	c := query[i]
	switch c {
	case '\'', '"', '`':
		var b strings.Builder
		for j := i + 1; j < len(query); j++ {
			if query[j] != c {
				b.WriteByte(query[j])
				continue
			}
			if j+1 < len(query) && query[j+1] == c {
				b.WriteByte(c)
				j++
				continue
			}
			return b.String(), j + 1, true, nil
		}
		return "", 0, false, fmt.Errorf("unterminated quote in query")
	case '[':
		end := strings.IndexByte(query[i:], ']')
		if end < 0 {
			return "", 0, false, fmt.Errorf("unterminated quote in query")
		}
		return query[i+1 : i+end], i + end + 1, true, nil
	}
	if !isWordByte(c) {
		return string(c), i + 1, false, nil
	}
	j := i
	for j < len(query) && isWordByte(query[j]) {
		j++
	}
	return query[i:j], j, false, nil
}

// isWordByte matches SQLite's identifier characters, where every non-ASCII byte
// counts as part of a name.
func isWordByte(c byte) bool {
	return c >= 0x80 || c == '_' || c == '$' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
