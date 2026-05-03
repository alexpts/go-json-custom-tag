package serializer

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type scalarSample struct {
	String  string  `json_custom:"string"`
	Bool    bool    `json_custom:"bool"`
	Int     int     `json_custom:"int"`
	Int8    int8    `json_custom:"int8"`
	Uint    uint    `json_custom:"uint"`
	Float32 float32 `json_custom:"float32"`
	Float   float64 `json_custom:"float"`
}

type collectionSample struct {
	Strings []string       `json_custom:"strings"`
	Bools   []bool         `json_custom:"bools"`
	Ints    []int          `json_custom:"ints"`
	Uints   []uint         `json_custom:"uints"`
	Floats  []float64      `json_custom:"floats"`
	Nested  []scalarSample `json_custom:"nested"`
	Array   [2]int         `json_custom:"array"`
	Map     map[string]int `json_custom:"map"`
}

type pointerSample struct {
	Name *string `json_custom:"name,omitempty"`
}

type interfaceSample struct {
	Value any `json_custom:"value"`
}

type rawString string

func (s *rawString) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	*s = rawString(strings.ToUpper(value))
	return nil
}

type unmarshalerSample struct {
	Name rawString `json_custom:"name"`
}

func TestMarshalScalarsCollectionsAndPointers(t *testing.T) {
	name := "Ivan"
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "scalars",
			in: scalarSample{
				String:  "text",
				Bool:    true,
				Int:     -12,
				Int8:    8,
				Uint:    42,
				Float32: 1.25,
				Float:   3.5,
			},
			want: `{"string":"text","bool":true,"int":-12,"int8":8,"uint":42,"float32":1.25,"float":3.5}`,
		},
		{
			name: "collections",
			in: collectionSample{
				Strings: []string{"a", "b"},
				Bools:   []bool{true, false},
				Ints:    []int{-1, 2},
				Uints:   []uint{1, 2},
				Floats:  []float64{1.5, 2.5},
				Nested:  []scalarSample{{String: "nested"}},
				Array:   [2]int{7, 8},
				Map:     map[string]int{"one": 1},
			},
			want: `{"strings":["a","b"],"bools":[true,false],"ints":[-1,2],"uints":[1,2],"floats":[1.5,2.5],"nested":[{"string":"nested","bool":false,"int":0,"int8":0,"uint":0,"float32":0,"float":0}],"array":[7,8],"map":{"one":1}}`,
		},
		{
			name: "pointer",
			in:   pointerSample{Name: &name},
			want: `{"name":"Ivan"}`,
		},
		{
			name: "nil pointer",
			in:   pointerSample{Name: nil},
			want: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New("json_custom").Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			assertJSONEqual(t, got, []byte(tt.want))
		})
	}
}

func TestUnmarshalScalarsCollectionsAndPointers(t *testing.T) {
	t.Run("scalars", func(t *testing.T) {
		data := []byte(`{"string":"text","bool":true,"int":-12,"int8":8,"uint":42,"float32":1.25,"float":3.5}`)
		var got scalarSample

		if err := New("json_custom").Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		want := scalarSample{String: "text", Bool: true, Int: -12, Int8: 8, Uint: 42, Float32: 1.25, Float: 3.5}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Unmarshal() = %#v, want %#v", got, want)
		}
	})

	t.Run("collections", func(t *testing.T) {
		data := []byte(`{"strings":["a","b"],"bools":[true,false],"ints":[-1,2],"uints":[1,2],"floats":[1.5,2.5],"nested":[{"string":"nested"}],"array":[7,8],"map":{"one":1}}`)
		var got collectionSample

		if err := New("json_custom").Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		want := collectionSample{
			Strings: []string{"a", "b"},
			Bools:   []bool{true, false},
			Ints:    []int{-1, 2},
			Uints:   []uint{1, 2},
			Floats:  []float64{1.5, 2.5},
			Nested:  []scalarSample{{String: "nested"}},
			Array:   [2]int{7, 8},
			Map:     map[string]int{"one": 1},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Unmarshal() = %#v, want %#v", got, want)
		}
	})

	t.Run("pointer and null", func(t *testing.T) {
		var got pointerSample
		if err := New("json_custom").Unmarshal([]byte(`{"name":"Ivan"}`), &got); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if got.Name == nil || *got.Name != "Ivan" {
			t.Fatalf("Unmarshal() pointer = %#v", got.Name)
		}

		if err := New("json_custom").Unmarshal([]byte(`{"name":null}`), &got); err != nil {
			t.Fatalf("Unmarshal(null) error = %v", err)
		}
		if got.Name != nil {
			t.Fatalf("Unmarshal(null) pointer = %#v, want nil", got.Name)
		}
	})
}

func TestUnmarshalInterfaceAndCustomUnmarshaler(t *testing.T) {
	t.Run("interface", func(t *testing.T) {
		var got interfaceSample
		if err := New("json_custom").Unmarshal([]byte(`{"value":{"x":1}}`), &got); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		want := interfaceSample{Value: map[string]any{"x": json.Number("1")}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Unmarshal() = %#v, want %#v", got, want)
		}
	})

	t.Run("custom unmarshaler", func(t *testing.T) {
		var got unmarshalerSample
		if err := New("json_custom").Unmarshal([]byte(`{"name":"ivan"}`), &got); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if got.Name != "IVAN" {
			t.Fatalf("custom unmarshaler value = %q, want %q", got.Name, "IVAN")
		}
	})
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "marshal nil", run: func() error { _, err := New("json_custom").Marshal(nil); return err }},
		{name: "unmarshal nil target", run: func() error { return New("json_custom").Unmarshal([]byte(`{}`), nil) }},
		{name: "unmarshal non pointer", run: func() error { var v scalarSample; return New("json_custom").Unmarshal([]byte(`{}`), v) }},
		{name: "invalid json", run: func() error { var v scalarSample; return New("json_custom").Unmarshal([]byte(`{`), &v) }},
		{name: "wrong scalar type", run: func() error { var v scalarSample; return New("json_custom").Unmarshal([]byte(`{"string":1}`), &v) }},
		{name: "negative unsigned", run: func() error { var v scalarSample; return New("json_custom").Unmarshal([]byte(`{"uint":-1}`), &v) }},
		{name: "wrong struct source", run: func() error { var v scalarSample; return New("json_custom").Unmarshal([]byte(`[]`), &v) }},
		{name: "wrong slice source", run: func() error {
			var v collectionSample
			return New("json_custom").Unmarshal([]byte(`{"strings":{}}`), &v)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("error = nil, want non-nil")
			}
		})
	}
}

func TestAppendJSONMarshalFallback(t *testing.T) {
	got, err := New("json_custom").Marshal(map[int]string{1: "one"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	assertJSONEqual(t, got, []byte(`{"1":"one"}`))
}

func TestMarshalPointersAndDefaultTagName(t *testing.T) {
	type sample struct {
		Name *string `json_custom:"name"`
		Raw  string
	}

	name := "Ivan"
	got, err := New("json_custom").Marshal(&sample{Name: &name, Raw: "fallback"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	assertJSONEqual(t, got, []byte(`{"name":"Ivan","Raw":"fallback"}`))
}

func TestMarshalFallbackError(t *testing.T) {
	_, err := New("json_custom").Marshal(map[int]func(){1: func() {}})
	if err == nil {
		t.Fatal("Marshal() error = nil, want non-nil")
	}
}

func TestErrorIsReturnedFromCustomMarshaler(t *testing.T) {
	_, err := New("json_custom").Marshal(failingMarshaler{})
	if err == nil {
		t.Fatal("Marshal() error = nil, want non-nil")
	}
}

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("boom")
}
