// Package leaf flattens JSON objects into leaves – every value that is not an
// object (strings, numbers, booleans, null, arrays) – keyed by a path such as
// "/claims/bh/submitted/date", and builds objects back from leaves. It is the
// basis for merging two versions of a record field by field.
package leaf

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
)

// ErrNotObject means the JSON value is not an object.
var ErrNotObject = errors.New("JSON value is not an object")

// Flatten returns the leaves of a JSON object with their compact JSON values.
func Flatten(data json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(data) == 0 {
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, ErrNotObject
	}
	return out, walk("", obj, out)
}

func walk(prefix string, obj map[string]any, out map[string]json.RawMessage) error {
	for k, v := range obj {
		p := prefix + "/" + escape(k)
		if child, ok := v.(map[string]any); ok {
			if err := walk(p, child, out); err != nil {
				return err
			}
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out[p] = raw
	}
	return nil
}

// Unflatten rebuilds a JSON object from leaves.
func Unflatten(leaves map[string]json.RawMessage) (json.RawMessage, error) {
	root := map[string]any{}
	for _, p := range slices.Sorted(maps.Keys(leaves)) {
		parts := splitPath(p)
		node := root
		for i, part := range parts {
			if i == len(parts)-1 {
				node[part] = leaves[p]
				break
			}
			child, ok := node[part].(map[string]any)
			if !ok {
				child = map[string]any{}
				node[part] = child
			}
			node = child
		}
	}
	return json.Marshal(root)
}

func escape(k string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(k) }

func splitPath(p string) []string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(s)
	}
	return parts
}

// Rebase applies the leaves that differ between base and mine onto theirs:
// the changes someone made to an old version are carried over to the
// current one, and everything they did not touch keeps its current value.
func Rebase(base, mine, theirs json.RawMessage) (json.RawMessage, error) {
	fb, err := Flatten(base)
	if err != nil {
		return nil, err
	}
	fm, err := Flatten(mine)
	if err != nil {
		return nil, err
	}
	out, err := Flatten(theirs)
	if err != nil {
		return nil, err
	}
	for p := range union(fb, fm) {
		vb, inBase := fb[p]
		vm, inMine := fm[p]
		if inBase == inMine && bytes.Equal(vb, vm) {
			continue // not changed by me
		}
		if inMine {
			out[p] = vm
		} else {
			delete(out, p)
		}
	}
	return Unflatten(out)
}

func union(a, b map[string]json.RawMessage) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
