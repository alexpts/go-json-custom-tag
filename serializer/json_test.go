package serializer

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alexpts/go-json-custom-tag/test/model"
)

type account struct {
	ID   int    `json:"id" json_snake:"id" json_camel:"id"`
	Name string `json:"name" json_snake:"name" json_camel:"name"`
}

type profile struct {
	User    model.User `json:"user" json_snake:"user" json_camel:"user"`
	Account account    `json:"account" json_snake:"account" json_camel:"account"`
}

type taggedSample struct {
	Name     string   `json:"name" json_snake:"name" json_camel:"name"`
	LastName string   `json:"lastName" json_snake:"last_name" json_camel:"lastName"`
	Age      int      `json:"age,omitempty" json_snake:"age,omitempty" json_camel:"age,omitempty"`
	Secret   string   `json:"-" json_snake:"-" json_camel:"-"`
	Tags     []string `json:"tags" json_snake:"tags" json_camel:"tags"`
}

func TestMarshalWithTags(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		in   any
		want string
	}{
		{
			name: "user snake",
			tag:  "json_snake",
			in:   model.User{LastName: "Ivanov"},
			want: `{"last_name":"Ivanov"}`,
		},
		{
			name: "user camel",
			tag:  "json_camel",
			in:   model.User{LastName: "Ivanov"},
			want: `{"lastName":"Ivanov"}`,
		},
		{
			name: "sample snake omitempty and skip",
			tag:  "json_snake",
			in: taggedSample{
				Name:     "Ivan",
				LastName: "Ivanov",
				Secret:   "hidden",
				Tags:     []string{"go", "json"},
			},
			want: `{"name":"Ivan","last_name":"Ivanov","tags":["go","json"]}`,
		},
		{
			name: "sample camel includes age",
			tag:  "json_camel",
			in: taggedSample{
				Name:     "Ivan",
				LastName: "Ivanov",
				Age:      31,
				Secret:   "hidden",
				Tags:     []string{"go"},
			},
			want: `{"name":"Ivan","lastName":"Ivanov","age":31,"tags":["go"]}`,
		},
		{
			name: "nil slice",
			tag:  "json_snake",
			in: taggedSample{
				Name:     "Ivan",
				LastName: "Ivanov",
				Tags:     nil,
			},
			want: `{"name":"Ivan","last_name":"Ivanov","tags":null}`,
		},
		{
			name: "empty slice",
			tag:  "json_snake",
			in: taggedSample{
				Name:     "Ivan",
				LastName: "Ivanov",
				Tags:     []string{},
			},
			want: `{"name":"Ivan","last_name":"Ivanov","tags":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.tag).Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			assertJSONEqual(t, got, []byte(tt.want))
		})
	}
}

func TestUnmarshalWithTags(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		data string
		want model.User
	}{
		{
			name: "snake",
			tag:  "json_snake",
			data: `{"last_name":"Petrov"}`,
			want: model.User{LastName: "Petrov"},
		},
		{
			name: "camel",
			tag:  "json_camel",
			data: `{"lastName":"Sidorov"}`,
			want: model.User{LastName: "Sidorov"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got model.User
			if err := New(tt.tag).Unmarshal([]byte(tt.data), &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Unmarshal() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStandardPackageAPI(t *testing.T) {
	tests := []struct {
		name string
		in   model.User
		want string
	}{
		{
			name: "user",
			in:   model.User{LastName: "Volkov"},
			want: `{"lastName":"Volkov"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			assertJSONEqual(t, data, []byte(tt.want))

			var out model.User
			if err := Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if !reflect.DeepEqual(out, tt.in) {
				t.Fatalf("Unmarshal() = %#v, want %#v", out, tt.in)
			}
		})
	}
}

func TestMarshalUnmarshalNestedStruct(t *testing.T) {
	enc := New("json_snake")
	input := profile{
		User:    model.User{LastName: "Smirnov"},
		Account: account{ID: 7, Name: "core"},
	}

	data, err := enc.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var gotMap map[string]any
	if err := json.Unmarshal(data, &gotMap); err != nil {
		t.Fatalf("json.Unmarshal marshal output error = %v", err)
	}

	wantMap := map[string]any{
		"user": map[string]any{
			"last_name": "Smirnov",
		},
		"account": map[string]any{
			"id":   float64(7),
			"name": "core",
		},
	}
	if !reflect.DeepEqual(gotMap, wantMap) {
		t.Fatalf("marshal map = %#v, want %#v", gotMap, wantMap)
	}

	var roundtrip profile
	if err := enc.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !reflect.DeepEqual(roundtrip, input) {
		t.Fatalf("roundtrip = %#v, want %#v", roundtrip, input)
	}
}

func TestPackageLevelAPI(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		in   model.User
		want string
	}{
		{
			name: "snake",
			tag:  "json_snake",
			in:   model.User{LastName: "Volkov"},
			want: `{"last_name":"Volkov"}`,
		},
		{
			name: "camel",
			tag:  "json_camel",
			in:   model.User{LastName: "Volkov"},
			want: `{"lastName":"Volkov"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalWithTag(tt.in, tt.tag)
			if err != nil {
				t.Fatalf("MarshalWithTag() error = %v", err)
			}
			assertJSONEqual(t, data, []byte(tt.want))

			var out model.User
			if err := UnmarshalWithTag(data, &out, tt.tag); err != nil {
				t.Fatalf("UnmarshalWithTag() error = %v", err)
			}

			if !reflect.DeepEqual(out, tt.in) {
				t.Fatalf("UnmarshalWithTag() = %#v, want %#v", out, tt.in)
			}
		})
	}
}

func assertJSONEqual(t *testing.T, got []byte, want []byte) {
	t.Helper()

	var gotJSON any
	if err := json.Unmarshal(got, &gotJSON); err != nil {
		t.Fatalf("got is not valid JSON: %s: %v", got, err)
	}

	var wantJSON any
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatalf("want is not valid JSON: %s: %v", want, err)
	}

	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("JSON = %#v, want %#v; raw got %s", gotJSON, wantJSON, got)
	}
}
