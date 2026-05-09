package serializer

import (
	"encoding/json"
	"testing"

	"github.com/alexpts/go-json-custom-tag/test/model"
)

type benchProfile struct {
	User    model.User `json:"user" json_snake:"user" json_camel:"user"`
	Age     int        `json:"age" json_snake:"age" json_camel:"age"`
	Active  bool       `json:"active" json_snake:"active" json_camel:"active"`
	Tags    []string   `json:"tags" json_snake:"tags" json_camel:"tags"`
	Aliases []string   `json:"aliases" json_snake:"aliases" json_camel:"aliases"`
}

func benchPayload() benchProfile {
	return benchProfile{
		User:    model.User{LastName: "Ivanov"},
		Age:     31,
		Active:  true,
		Tags:    []string{"core", "billing", "ops", "go", "json"},
		Aliases: []string{"ivan", "ivanov", "i.v."},
	}
}

func BenchmarkMarshalStdJSON(b *testing.B) {
	v := benchPayload()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(v)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalStdJSONPointer(b *testing.B) {
	v := benchPayload()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(&v)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalCustomSnake_WarmCache(b *testing.B) {
	v := benchPayload()
	enc := New("json_snake")

	_, _ = enc.Marshal(v)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := enc.Marshal(v)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalCustomSnakePointer_WarmCache(b *testing.B) {
	v := benchPayload()
	enc := New("json_snake")

	_, _ = enc.Marshal(&v)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := enc.Marshal(&v)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalCustomSnake_ColdLike(b *testing.B) {
	v := benchPayload()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc := New("json_snake")
		_, err := enc.Marshal(v)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStdJSON(b *testing.B) {
	data, err := json.Marshal(benchPayload())
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var v benchProfile
		if err := json.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalCustomSnake_WarmCache(b *testing.B) {
	enc := New("json_snake")
	data, err := enc.Marshal(benchPayload())
	if err != nil {
		b.Fatal(err)
	}

	var warm benchProfile
	_ = enc.Unmarshal(data, &warm)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var v benchProfile
		if err := enc.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}
