package graph

import (
	"encoding/json"
	"math/big"
	"strings"
	"unicode/utf8"
)

// FieldsInput is the `fields` member of an input: each Field name maps to its
// JSON value as encoding/json decodes it with UseNumber, or to nil for JSON
// null, which gives the Field no value.
type FieldsInput map[string]any

// hasField reports whether Nodes of Type t have the Field name.
func hasField(t NodeType, name string) bool {
	switch name {
	case "product", "title", "description", "outputs", "status", "assignee":
		return true
	case "slug":
		return t == TypeProduct
	}
	return false
}

func requiredField(name string) bool {
	return name == "product" || name == "title" || name == "outputs" || name == "status" || name == "slug"
}

// checkFields runs the steps of "check the Fields" of the base
// specification. creating is true if the operation creates a Node.
func checkFields(t NodeType, in FieldsInput, creating bool) error {
	for name := range in {
		if !hasField(t, name) {
			return invalid("a %s has no Field %q", t, name)
		}
	}
	for name := range in {
		if name == "product" || name == "slug" {
			return invalid("the server sets the Field %q", name)
		}
	}
	for name, v := range in {
		if v == nil {
			continue
		}
		if err := checkFieldValue(name, v); err != nil {
			return err
		}
	}
	for name, v := range in {
		if v == nil && requiredField(name) {
			return invalid("the Field %q is required, so it must have a value", name)
		}
	}
	if _, ok := in["title"]; creating && !ok {
		return invalid("the Field \"title\" is required")
	}
	return nil
}

func checkFieldValue(name string, v any) error {
	switch name {
	case "title":
		if s, ok := v.(string); !ok || s == "" {
			return invalid("the Field \"title\" must be a string of at least 1 character")
		}
	case "description":
		if _, ok := v.(string); !ok {
			return invalid("the Field \"description\" must be a string")
		}
	case "status":
		if s, ok := v.(string); !ok || !Status(s).Valid() {
			return invalid("the Field \"status\" must be not_started, in_progress, or done")
		}
	case "assignee":
		if s, ok := v.(string); !ok || s == "" {
			return invalid("the Field \"assignee\" must be a user ID of at least 1 character")
		}
	case "outputs":
		_, err := parseOutputs(v)
		return err
	}
	return nil
}

func parseOutputs(v any) ([]Output, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, invalid("the Field \"outputs\" must be an array of Outputs")
	}
	outputs := make([]Output, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, invalid("each Output must be an object")
		}
		var o Output
		for name, mv := range obj {
			switch name {
			case "outputType":
				s, ok := mv.(string)
				if !ok || s == "" {
					return nil, invalid("outputType must be a string of at least 1 character")
				}
				o.OutputType = s
			case "title":
				s, ok := mv.(string)
				if !ok || s == "" {
					return nil, invalid("the title of an Output must be a string of at least 1 character")
				}
				o.Title = &s
			case "payload":
				if _, ok := mv.(map[string]any); !ok {
					return nil, invalid("the payload of an Output must be an object")
				}
				o.Payload = mv
			default:
				return nil, invalid("an Output has no member %q", name)
			}
		}
		if o.OutputType == "" {
			return nil, invalid("each Output must give outputType")
		}
		outputs = append(outputs, o)
	}
	return outputs, nil
}

// applyFields sets each Field that in names, or removes its value. in must
// have passed checkFields.
func applyFields(f *Fields, in FieldsInput) {
	for name, v := range in {
		s, _ := v.(string)
		switch name {
		case "title":
			f.Title = s
		case "description":
			f.Description = optString(v)
		case "status":
			f.Status = Status(s)
		case "assignee":
			f.Assignee = optString(v)
		case "outputs":
			f.Outputs, _ = parseOutputs(v)
		}
	}
}

func optString(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// ── Sameness ────────────────────────────────────────────────────────────────

func fieldsSame(a, b Fields) bool {
	if a.Product != b.Product || a.Title != b.Title || a.Status != b.Status ||
		!optSame(a.Description, b.Description) || !optSame(a.Assignee, b.Assignee) || !optSame(a.Slug, b.Slug) ||
		len(a.Outputs) != len(b.Outputs) {
		return false
	}
	for i := range a.Outputs {
		x, y := a.Outputs[i], b.Outputs[i]
		if x.OutputType != y.OutputType || !optSame(x.Title, y.Title) || (x.Payload == nil) != (y.Payload == nil) {
			return false
		}
		if x.Payload != nil && !jsonSame(x.Payload, y.Payload) {
			return false
		}
	}
	return true
}

func optSame(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// jsonSame reports whether 2 decoded JSON values are the same, as the extended
// specification defines it: numbers are compared as exact decimal numbers, and
// object members in any order.
func jsonSame(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case json.Number:
		y, ok := b.(json.Number)
		return ok && decimalKey(string(x)) == decimalKey(string(y))
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonSame(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !jsonSame(xv, yv) {
				return false
			}
		}
		return true
	}
	return false
}

// decimalKey returns one text for all the JSON numbers that are equal as exact
// decimal numbers (e.g. 1, 1.0, and 10e-1): the sign, the significant digits,
// and the exponent of the last digit. It doesn't compute the value, so a huge
// exponent costs no more than its digits.
func decimalKey(num string) string {
	sign := ""
	if strings.HasPrefix(num, "-") {
		sign, num = "-", num[1:]
	}
	exp := new(big.Int)
	if i := strings.IndexAny(num, "eE"); i >= 0 {
		exp.SetString(strings.TrimPrefix(num[i+1:], "+"), 10)
		num = num[:i]
	}
	intPart, frac, _ := strings.Cut(num, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	exp.Sub(exp, big.NewInt(int64(len(frac))))
	trimmed := strings.TrimRight(digits, "0")
	exp.Add(exp, big.NewInt(int64(len(digits)-len(trimmed))))
	if trimmed == "" {
		return "0"
	}
	return sign + trimmed + "e" + exp.String()
}

// ValidUserID reports whether s is a valid UserId: a string of at least 1
// character.
func ValidUserID(s string) bool { return s != "" && utf8.ValidString(s) }
