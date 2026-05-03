package serializer

import (
	"encoding/json"
	"reflect"
	"testing"

	"go-json-presenter/internal/model"
)

type account struct {
	ID   int    `json:"id" json_snake:"id" json_camel:"id"`
	Name string `json:"name" json_snake:"name" json_camel:"name"`
}

type profile struct {
	User    model.User `json:"user" json_snake:"user" json_camel:"user"`
	Account account    `json:"account" json_snake:"account" json_camel:"account"`
}

func TestMarshalWithSnakeTag(t *testing.T) {
	enc := NewJsonEncoder("json_snake")
	user := model.User{LastName: "Ivanov"}

	got, err := enc.Marshal(user)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"last_name":"Ivanov"}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestMarshalWithStandardJSONTag(t *testing.T) {
	enc := New("json")
	user := model.User{LastName: "Ivanov"}

	got, err := enc.Marshal(user)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"lastName":"Ivanov"}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestUnmarshalWithSnakeTag(t *testing.T) {
	enc := NewJsonEncoder("json_snake")
	data := []byte(`{"last_name":"Petrov"}`)

	var user model.User
	if err := enc.Unmarshal(data, &user); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if user.LastName != "Petrov" {
		t.Fatalf("Unmarshal() user.LastName = %q, want %q", user.LastName, "Petrov")
	}
}

func TestUnmarshalWithCamelTag(t *testing.T) {
	enc := New("json_camel")
	data := []byte(`{"lastName":"Sidorov"}`)

	var user model.User
	if err := enc.Unmarshal(data, &user); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if user.LastName != "Sidorov" {
		t.Fatalf("Unmarshal() user.LastName = %q, want %q", user.LastName, "Sidorov")
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
	user := model.User{LastName: "Volkov"}

	data, err := MarshalWithTag(user, "json_snake")
	if err != nil {
		t.Fatalf("MarshalWithTag() error = %v", err)
	}
	if string(data) != `{"last_name":"Volkov"}` {
		t.Fatalf("MarshalWithTag() = %s", data)
	}

	var out model.User
	if err := UnmarshalWithTag(data, &out, "json_snake"); err != nil {
		t.Fatalf("UnmarshalWithTag() error = %v", err)
	}
	if out.LastName != "Volkov" {
		t.Fatalf("UnmarshalWithTag() user.LastName = %q, want %q", out.LastName, "Volkov")
	}
}

func TestPackageLevelStandardAPI(t *testing.T) {
	user := model.User{LastName: "Volkov"}

	data, err := Marshal(user)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(data) != `{"lastName":"Volkov"}` {
		t.Fatalf("Marshal() = %s", data)
	}

	var out model.User
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if out.LastName != "Volkov" {
		t.Fatalf("Unmarshal() user.LastName = %q, want %q", out.LastName, "Volkov")
	}
}
