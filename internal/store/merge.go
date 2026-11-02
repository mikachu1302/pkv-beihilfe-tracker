package store

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/leaf"
)

// Every leaf of a record (see package leaf) has a timestamp in the clock; a
// merge takes, leaf by leaf, the value with the newer timestamp. Arrays are
// leaves as a whole.

// compareStamps orders clock values "2006-01-02T15:04:05.000Z@device". The
// time part has a fixed width, so string order is time order; the device ID
// breaks ties. A missing stamp is the oldest.
func compareStamps(a, b string) int { return strings.Compare(a, b) }

// stamp returns base updated to data: every leaf that differs from base gets ts.
func stamp(base envelope, kind Kind, data json.RawMessage, ts string) (envelope, error) {
	newLeaves, err := leaf.Flatten(data)
	if err != nil {
		return envelope{}, err
	}
	oldLeaves, err := leaf.Flatten(base.Data)
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
	la, err := leaf.Flatten(a.Data)
	if err != nil {
		return envelope{}, err
	}
	lb, err := leaf.Flatten(b.Data)
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
	data, err := leaf.Unflatten(leaves)
	if err != nil {
		return envelope{}, err
	}
	return envelope{Schema: max(a.Schema, b.Schema), Type: a.Type, Data: data, Clock: clock}, nil
}
