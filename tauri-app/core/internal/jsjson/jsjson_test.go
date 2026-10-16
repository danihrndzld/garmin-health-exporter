package jsjson

import (
	"fmt"
	"math"
	"testing"
)

// Expected strings come from node -e 'console.log(String(n))'.
func TestFormatNumberMatchesJavaScript(t *testing.T) {
	cases := []struct {
		name string
		f    float64
		want string
	}{
		{"EP-301 zero", 0, "0"},
		{"EP-302 integers and fractions", 72.0, "72"},
		{"EP-303 negative", -1, "-1"},
		{"EP-304 fraction", 0.1, "0.1"},
		{"EP-305 seven digit integer, no exponent", 1234567, "1234567"},
		{"EP-306 eleven digit integer", 12345678901, "12345678901"},
		{"BVA-301 1e-6 is the last decimal form", 1e-6, "0.000001"},
		{"BVA-302 just under 1e-6 switches to exponent", 9.99999e-7, "9.99999e-7"},
		{"BVA-303 1e-7", 1e-7, "1e-7"},
		{"BVA-304 1.5e-7", 1.5e-7, "1.5e-7"},
		{"BVA-305 negative small", -2.5e-8, "-2.5e-8"},
		{"BVA-306 1e21 is the first exponent form", 1e21, "1e+21"},
		{"BVA-307 1.5e21", 1.5e21, "1.5e+21"},
		{"BVA-308 just under 1e21 stays decimal", 123456789012345680000, "123456789012345680000"},
		{"BVA-309 negative zero prints 0", math.Copysign(0, -1), "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatNumber(c.f); got != c.want {
				t.Errorf("FormatNumber(%v) = %q, want %q", c.f, got, c.want)
			}
		})
	}
	t.Run("EP-307 runtime sum 0.1+0.2", func(t *testing.T) {
		a, b := 0.1, 0.2 // a constant sum would be folded to exactly 0.3
		if got := FormatNumber(a + b); got != "0.30000000000000004" {
			t.Errorf("got %q", got)
		}
	})
}

func TestParseKeepsJavaScriptKeyOrder(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		// Index-like keys first in numeric order; "01" and 2^32-1 are not index-like; a repeated key keeps its first place.
		{"EP-308 index keys first, repeat keeps place", `{"b":1,"a":2,"1001":3,"10":4,"01":5,"4294967295":6,"b":7}`, []string{"10", "1001", "b", "a", "01", "4294967295"}},
		{"BVA-310 0 and 2^32-2 are index-like", `{"b":1,"4294967294":8,"0":9}`, []string{"0", "4294967294", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := Parse([]byte(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(v.O.Keys) != fmt.Sprint(c.want) {
				t.Fatalf("keys %v, want %v", v.O.Keys, c.want)
			}
		})
	}
	t.Run("EP-309 the last value of a repeated key wins", func(t *testing.T) {
		v, _ := Parse([]byte(`{"b":1,"b":7}`))
		if v.Get("b").N != 7 {
			t.Fatal("want 7")
		}
	})
	t.Run("EP-310 Get on absent key and non-object is Undefined", func(t *testing.T) {
		v, _ := Parse([]byte(`{"a":1}`))
		if v.Get("x").Kind != Undefined || Num(1).Get("a").Kind != Undefined || !v.Get("x").IsNullish() {
			t.Fatal("want Undefined")
		}
	})
	t.Run("EP-311 Set keeps the first position", func(t *testing.T) {
		o := NewObj()
		o.Set("b", Num(1))
		o.Set("a", Num(2))
		o.Set("b", Num(3))
		if fmt.Sprint(o.Keys) != "[b a]" || o.Get("b").N != 3 {
			t.Fatalf("%v %v", o.Keys, o.Get("b"))
		}
	})
	t.Run("EP-312 invalid JSON is an error", func(t *testing.T) {
		if _, err := Parse([]byte(`{"a":`)); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestParseReadsSourceNumberTextAsFloat(t *testing.T) {
	v, _ := Parse([]byte(`{"x":72.0,"y":1E3}`))
	if Cell(v.Get("x")) != "72" || Cell(v.Get("y")) != "1000" {
		t.Fatalf("got %q %q", Cell(v.Get("x")), Cell(v.Get("y")))
	}
}

// Expected strings come from node: JSON.stringify(JSON.parse(input)).
func TestStringify(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"CL-301 quote, newline, control, no HTML escape",
			"{\"z\":[1,null,true,\"a\\\"b\"],\"t\":\"<&> \\u0001\\n\",\"n\":{\"2\":0.5,\"k\":-0}}",
			"{\"z\":[1,null,true,\"a\\\"b\"],\"t\":\"<&> \\u0001\\n\",\"n\":{\"2\":0.5,\"k\":0}}"},
		{"CL-302 b f r t backslash, slash and non-ASCII stay literal",
			"[\"\\b\\f\\r\\t\\\\/é\"]", "[\"\\b\\f\\r\\t\\\\/é\"]"},
		{"CL-303 empty containers", `{"a":[],"b":{}}`, `{"a":[],"b":{}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := Parse([]byte(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if got := Stringify(v); got != c.want {
				t.Fatalf("got  %s\nwant %s", got, c.want)
			}
		})
	}
	t.Run("CL-304 U+2028 and DEL are not escaped", func(t *testing.T) {
		if got := Stringify(Str(" \x7f")); got != "\" \x7f\"" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("CL-305 undefined inside an array is null", func(t *testing.T) {
		if got := Stringify(Value{Kind: Array, A: []Value{{}}}); got != "[null]" {
			t.Fatalf("got %s", got)
		}
	})
}

func TestIndent(t *testing.T) {
	v, _ := Parse([]byte(`{"a":[],"b":{},"c":[1,{"d":null}]}`))
	want := "{\n  \"a\": [],\n  \"b\": {},\n  \"c\": [\n    1,\n    {\n      \"d\": null\n    }\n  ]\n}"
	t.Run("CL-306 two spaces, empty containers inline", func(t *testing.T) {
		if got := string(Indent(v)); got != want {
			t.Fatalf("got\n%s\nwant\n%s", got, want)
		}
	})
}

func TestCellMatchesJavaScriptString(t *testing.T) {
	arr, _ := Parse([]byte(`[1,null,"a",[2,3],[],{"a":1}]`))
	obj, _ := Parse([]byte(`{"a":1}`))
	cases := []struct {
		name string
		v    Value
		want string
	}{
		{"EP-313 undefined", Value{}, ""},
		{"EP-314 null", Value{Kind: Null}, ""},
		{"EP-315 true", Value{Kind: Bool, B: true}, "true"},
		{"EP-316 false", Value{Kind: Bool}, "false"},
		{"EP-317 string", Str("x"), "x"},
		{"EP-318 number", Num(3), "3"},
		{"EP-319 array joins, null empty, nested flat, object tagged", arr, "1,,a,2,3,,[object Object]"},
		{"EP-320 object", obj, "[object Object]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Cell(c.v); got != c.want {
				t.Errorf("Cell = %q, want %q", got, c.want)
			}
		})
	}
}
