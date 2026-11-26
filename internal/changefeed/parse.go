package changefeed

import (
	"github.com/d4nyll/nodoplex/internal/graph"
)

func invalid(format string, args ...any) error {
	return graph.Refuse(graph.CodeInvalidRequest, format, args...)
}

// ParseGroups reads the value of `filterGroups`, as encoding/json decodes it
// with UseNumber, and checks it against the FilterGroup and StateFilter
// schemas.
func ParseGroups(v any) ([]Group, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, invalid("filterGroups must be an array of filter groups")
	}
	groups := make([]Group, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok || len(obj) == 0 {
			return nil, invalid("each filter group must be an object with at least one member")
		}
		var g Group
		var err error
		for name, mv := range obj {
			switch name {
			case "type":
				g.Types, err = parseList(mv, "type", func(x any) (graph.NodeType, bool) {
					s, ok := x.(string)
					return graph.NodeType(s), ok && graph.NodeType(s).Valid()
				})
			case "watcher":
				g.Watchers, err = parseList(mv, "watcher", parseOptUserID)
			case "archived":
				g.Archived, err = parseStateFilter(mv, "archived", func(x any) (bool, bool) { b, ok := x.(bool); return b, ok })
			case "status":
				g.Status, err = parseStateFilter(mv, "status", func(x any) (graph.Status, bool) {
					s, ok := x.(string)
					return graph.Status(s), ok && graph.Status(s).Valid()
				})
			case "assignee":
				g.Assignee, err = parseStateFilter(mv, "assignee", parseOptUserID)
			default:
				return nil, invalid("a filter group has no member %q", name)
			}
			if err != nil {
				return nil, err
			}
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func parseOptUserID(x any) (*string, bool) {
	if x == nil {
		return nil, true
	}
	s, ok := x.(string)
	return &s, ok && graph.ValidUserID(s)
}

func parseList[T any](v any, name string, parse func(any) (T, bool)) ([]T, error) {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil, invalid("%s must be an array of at least 1 value", name)
	}
	out := make([]T, len(list))
	for i, x := range list {
		if out[i], ok = parse(x); !ok {
			return nil, invalid("%s holds a value that isn't valid", name)
		}
	}
	return out, nil
}

func parseStateFilter[T any](v any, name string, parse func(any) (T, bool)) (*StateFilter[T], error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, invalid("%s must be a StateFilter object", name)
	}
	f := &StateFilter[T]{}
	var values, before, after []T
	var err error
	for member, mv := range obj {
		switch member {
		case "check":
			s, _ := mv.(string)
			if s != "before" && s != "after" && s != "both" && s != "either" {
				return nil, invalid("%s.check must be before, after, both, or either", name)
			}
			f.Check = s
		case "values":
			values, err = parseList(mv, name+".values", parse)
		case "beforeValues":
			before, err = parseList(mv, name+".beforeValues", parse)
		case "afterValues":
			after, err = parseList(mv, name+".afterValues", parse)
		default:
			return nil, invalid("a StateFilter has no member %q", member)
		}
		if err != nil {
			return nil, err
		}
	}
	switch {
	case f.Check == "":
		return nil, invalid("%s.check is required", name)
	case values != nil && before == nil && after == nil:
		f.Before, f.After = values, values
	case values == nil && before != nil && after != nil:
		if f.Check != "both" && f.Check != "either" {
			return nil, invalid("%s can give beforeValues and afterValues only with the check both or either", name)
		}
		f.Before, f.After = before, after
	default:
		return nil, invalid("%s must give either values alone, or beforeValues and afterValues together", name)
	}
	return f, nil
}
