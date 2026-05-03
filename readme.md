# json-custom-tag

Позволяет использовать кастомные структурные теги при сериализации в json

```go

import (
    " github.com/alexpts/go-json-custom-tag/serializer"
)


type User struct {
    LastName string `json:"lastName" json_snake:"last_name" json_camel:"lastName" custom_json:"ln"`
}

encoder := serializer.New("custom_json")

model := User{LastName: "Ivanov"}
bytes, err := encoder.Marshal(&model)

```