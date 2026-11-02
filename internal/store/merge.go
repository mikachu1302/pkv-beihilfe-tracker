package store

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// A record's data is flattened into leaves: every value that is not an
// object (strings, numbers, booleans, null, arrays) gets a path such as
// "/claims/bh/submitted/date". Each leaf has a timestamp in the clock; a merge
// takes, leaf by leaf, the value with the newer timestamp. Arrays are leaves
// as a whole.

// flatten returns the leaves of a JSON object with their compact JSON values.
func flatten(data json.RawMessage) (map[string]json.RawMessage, error) {
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
		return nil, errNotObject
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

// unflatten rebuilds a JSON object from leaves.
func unflatten(leaves map[string]json.RawMessage) (json.RawMessage, error) {
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

// compareStamps orders clock values "2006-01-02T15:04:05.000Z@device". The
// time part has a fixed width, so string order is time order; the device ID
// breaks ties. A missing stamp is the oldest.
func compareStamps(a, b string) int { return strings.Compare(a, b) }

// stamp returns base updated to data: every leaf that differs from base gets ts.
func stamp(base envelope, kind Kind, data json.RawMessage, ts string) (envelope, error) {
	newLeaves, err := flatten(data)
	if err != nil {
		return envelope{}, err
	}
	oldLeaves, err := flatten(base.Data)
	if err != nil {
		return envelope{}, err
	}
	clock := maps.Clone(base.Clock)
	if clock == nil {
		clock = map[string]string{}
	}
	for p, v := range newLeaves {
		if o, ok := oldLeaves[p]; !ok || !bytes.Equal(o, v) {
			clock[p] = ts
		}
	}
	for p := range oldLeaves {
		if _, ok := newLeaves[p]; !ok {
			clock[p] = ts // removed: remember when, so the removal wins over older values
		}
	}
	return envelope{Schema: SchemaVersion, Type: kind, Data: data, Clock: clock}, nil
}

// merge combines two versions of the same record leaf by leaf.
func merge(a, b envelope) (envelope, error) {
	la, err := flatten(a.Data)
	if err != nil {
		return envelope{}, err
	}
	lb, err := flatten(b.Data)
	if err != nil {
		return envelope{}, err
	}
	paths := map[string]bool{}
	for _, m := range []map[string]json.RawMessage{la, lb} {
		for p := range m {
			paths[p] = true
		}
	}
	for _, m := range []map[string]string{a.Clock, b.Clock} {
		for p := range m {
			paths[p] = true
		}
	}
	leaves := map[string]json.RawMessage{}
	clock := map[string]string{}
	for p := range paths {
		va, okA := la[p]
		vb, okB := lb[p]
		useA := false
		switch c := compareStamps(a.Clock[p], b.Clock[p]); {
		case c > 0:
			useA = true
		case c == 0:
			// Same stamp (or none): prefer a present value, then the larger one, so
			// every device decides the same way.
			useA = okA && (!okB || bytes.Compare(va, vb) >= 0)
		}
		if useA {
			if okA {
				leaves[p] = va
			}
			if st := a.Clock[p]; st != "" {
				clock[p] = st
			}
		} else {
			if okB {
				leaves[p] = vb
			}
			if st := b.Clock[p]; st != "" {
				clock[p] = st
			}
		}
	}
	data, err := unflatten(leaves)
	if err != nil {
		return envelope{}, err
	}
	return envelope{Schema: max(a.Schema, b.Schema), Type: a.Type, Data: data, Clock: clock}, nil
}
