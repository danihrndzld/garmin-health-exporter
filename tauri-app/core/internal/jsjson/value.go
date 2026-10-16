// Package jsjson models JavaScript values the way the CSV writer needs them: property order,
// String(n) for numbers, and JSON.stringify.
package jsjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
)

type Kind uint8

const (
	Undefined Kind = iota
	Null
	Bool
	Number
	String
	Array
	Object
)

type Value struct {
	Kind Kind
	B    bool
	N    float64
	S    string
	A    []Value
	O    *Obj
}

// Obj keeps JavaScript property order in Keys.
type Obj struct {
	Keys []string
	m    map[string]Value
}

func NewObj() *Obj { return &Obj{m: map[string]Value{}} }

// Get returns Undefined when k is absent.
func (o *Obj) Get(k string) Value { return o.m[k] }

// Set keeps the first position of k, like JavaScript assignment. Index-like keys go first in numeric order.
func (o *Obj) Set(k string, v Value) {
	if _, ok := o.m[k]; !ok {
		o.Keys = append(o.Keys, k)
		if isIndex(k) {
			sortKeys(o.Keys)
		}
	}
	o.m[k] = v
}

// Get is Undefined unless v is an object.
func (v Value) Get(k string) Value {
	if v.Kind != Object {
		return Value{}
	}
	return v.O.Get(k)
}

// IsNullish is true for Undefined and Null, for ?? and || {}.
func (v Value) IsNullish() bool { return v.Kind == Undefined || v.Kind == Null }

func Num(f float64) Value { return Value{Kind: Number, N: f} }
func Str(s string) Value  { return Value{Kind: String, S: s} }

func Parse(b []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return Value{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Value{}, fmt.Errorf("jsjson: trailing data")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return Value{}, err
	}
	switch t := tok.(type) {
	case nil:
		return Value{Kind: Null}, nil
	case bool:
		return Value{Kind: Bool, B: t}, nil
	case string:
		return Str(t), nil
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			err = nil // JSON.parse reads 1e400 as Infinity
		}
		return Num(f), err
	case json.Delim:
		if t == '[' {
			a := Value{Kind: Array, A: []Value{}}
			for dec.More() {
				e, err := parseValue(dec)
				if err != nil {
					return Value{}, err
				}
				a.A = append(a.A, e)
			}
			_, err := dec.Token()
			return a, err
		}
		o := NewObj()
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return Value{}, err
			}
			e, err := parseValue(dec)
			if err != nil {
				return Value{}, err
			}
			o.Set(kt.(string), e)
		}
		if _, err := dec.Token(); err != nil {
			return Value{}, err
		}
		return Value{Kind: Object, O: o}, nil
	}
	return Value{}, fmt.Errorf("jsjson: unexpected token %v", tok)
}

// isIndex reports whether k is a canonical array index, which JavaScript orders first.
func isIndex(k string) bool {
	if k == "0" {
		return true
	}
	if k == "" || k[0] < '1' || k[0] > '9' {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 1<<32-1
}

// sortKeys puts index-like keys first in numeric order; the rest keep insertion order.
func sortKeys(keys []string) {
	sort.SliceStable(keys, func(i, j int) bool {
		ii, ij := isIndex(keys[i]), isIndex(keys[j])
		if ii && ij {
			a, _ := strconv.ParseUint(keys[i], 10, 64)
			b, _ := strconv.ParseUint(keys[j], 10, 64)
			return a < b
		}
		return ii && !ij
	})
}
