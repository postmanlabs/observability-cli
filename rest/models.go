package rest

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
)

type PostmanMetaData struct {
	CollectionID string `json:"collection_id"`
	Environment  string `json:"environment"`
}

// TODO: shouldn't this be in akita-cli/api_schema?
type Service struct {
	ID              akid.ServiceID  `json:"id"`
	Name            string          `json:"name"`
	PostmanMetaData PostmanMetaData `json:"postman_meta_data"`
}

type User = api_schema.UserResponse
