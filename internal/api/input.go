package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
)

// maxInputBytes is the size limit of a body, and of the value of the query
// parameter filterGroups (check 9 of the extended specification).
const maxInputBytes = 1 << 20

// maxJSONDepth is the largest depth of nested JSON arrays and objects.
const maxJSONDepth = 64

func invalid(format string, args ...any) error {
	return graph.Refuse(graph.CodeInvalidRequest, format, args...)
}

// percentDecode percent-decodes a text, as H5 gives. ok is false if the text
// isn't valid percent-encoding.
func percentDecode(s string) (string, bool) {
	if !strings.Contains(s, "%") {
		return s, utf8.ValidString(s)
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil || strings.ContainsAny(s[i+1:i+3], "+-") {
			return "", false
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), utf8.ValidString(b.String())
}

// parseQuery reads a query as H6 gives. It refuses a name or a value that
// isn't valid percent-encoding.
func parseQuery(raw string) (map[string][]string, error) {
	query := map[string][]string{}
	for _, param := range strings.Split(raw, "&") {
		if param == "" {
			continue
		}
		name, value, _ := strings.Cut(param, "=")
		n, ok1 := percentDecode(strings.ReplaceAll(name, "+", " "))
		v, ok2 := percentDecode(strings.ReplaceAll(value, "+", " "))
		if !ok1 || !ok2 {
			return nil, invalid("the query parameter %q isn't valid percent-encoding", name)
		}
		query[n] = append(query[n], v)
	}
	return query, nil
}

// decodeJSON reads a JSON text in UTF-8. It refuses a text that isn't JSON, a
// string that holds a code point from U+D800 to U+DFFF (J4), an object that
// has the same member name more than one time, and nesting deeper than
// maxJSONDepth. It decodes numbers as json.Number, so that each keeps its
// exact text.
func decodeJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, invalid("the JSON text isn't valid UTF-8")
	}
	if hasLoneSurrogate(data) {
		return nil, invalid("a JSON string holds a code point from U+D800 to U+DFFF")
	}
	if err := checkNamesAndDepth(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, invalid("the input isn't a JSON text")
	}
	return v, nil
}

// checkNamesAndDepth walks the tokens of a JSON text.
func checkNamesAndDepth(data []byte) error {
	type frame struct {
		object    bool
		expectKey bool
		names     map[string]bool
	}
	var stack []*frame
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	values := 0
	afterValue := func() {
		if len(stack) == 0 {
			values++
		} else if top := stack[len(stack)-1]; top.object {
			top.expectKey = true
		}
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || (len(stack) == 0 && values > 0) {
			return invalid("the input isn't a JSON text")
		}
		if len(stack) > 0 {
			if top := stack[len(stack)-1]; top.object && top.expectKey {
				if tok == json.Delim('}') {
					stack = stack[:len(stack)-1]
					afterValue()
					continue
				}
				name := tok.(string)
				if top.names[name] {
					return invalid("a JSON object has the member %q more than one time", name)
				}
				top.names[name] = true
				top.expectKey = false
				continue
			}
		}
		switch tok {
		case json.Delim('{'), json.Delim('['):
			if len(stack) == maxJSONDepth {
				return invalid("the JSON text nests deeper than %d", maxJSONDepth)
			}
			stack = append(stack, &frame{object: tok == json.Delim('{'), expectKey: true, names: map[string]bool{}})
		case json.Delim('}'), json.Delim(']'):
			stack = stack[:len(stack)-1]
			afterValue()
		default:
			afterValue()
		}
	}
	if values != 1 {
		return invalid("the input isn't a JSON text")
	}
	return nil
}

// hasLoneSurrogate reports whether a JSON string in data has a \u escape of
// a code point from U+D800 to U+DFFF that isn't half of a surrogate pair.
func hasLoneSurrogate(data []byte) bool {
	hex4 := func(i int) (rune, bool) {
		if i+6 > len(data) || data[i] != '\\' || data[i+1] != 'u' {
			return 0, false
		}
		v, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		return rune(v), err == nil
	}
	inString := false
	for i := 0; i < len(data); i++ {
		switch c := data[i]; {
		case !inString:
			inString = c == '"'
		case c == '"':
			inString = false
		case c == '\\':
			r, ok := hex4(i)
			if !ok {
				i++ // skip the escaped character
				continue
			}
			switch {
			case r >= 0xD800 && r <= 0xDBFF:
				low, ok := hex4(i + 6)
				if !ok || low < 0xDC00 || low > 0xDFFF {
					return true
				}
				i += 11
			case r >= 0xDC00 && r <= 0xDFFF:
				return true
			default:
				i += 5
			}
		}
	}
	return false
}

// ── Values ──────────────────────────────────────────────────────────────────

var jsonInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
var textInteger = regexp.MustCompile(`^-?[0-9]+$`)

// jsonInt reads an integer from a JSON number with no fraction part and no
// exponent part (J4).
func jsonInt(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok || !jsonInteger.MatchString(string(n)) {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	return i, err == nil
}

// textInt reads an integer from a path parameter, a query parameter, or a
// header (I7).
func textInt(s string) (int64, bool) {
	if !textInteger.MatchString(s) {
		return 0, false
	}
	i, err := strconv.ParseInt(s, 10, 64)
	return i, err == nil
}

// parseUUID reads the text form of a UUID: 32 hexadecimal digits in groups of
// 8, 4, 4, 4, and 12, with a `-` between each group and the next (J4, I7).
func parseUUID(s string) (uuid.UUID, bool) {
	if len(s) != 36 {
		return uuid.Nil, false
	}
	for i, c := range []byte(s) {
		isDash := i == 8 || i == 13 || i == 18 || i == 23
		isHex := c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
		if isDash != (c == '-') || !isDash && !isHex {
			return uuid.Nil, false
		}
	}
	id, err := uuid.Parse(s)
	return id, err == nil
}

func textBool(s string) (bool, bool) {
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// validSlug reports whether s is a valid slug (I2).
func validSlug(s string) bool {
	labels := strings.Split(s, ".")
	if len(s) > 253 || len(labels) < 3 {
		return false
	}
	for _, l := range labels {
		if len(l) < 1 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range []byte(l) {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
