package model

type User struct {
	LastName string `json:"lastName" json_snake:"last_name" json_camel:"lastName"`
}
