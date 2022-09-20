package rest

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
)

// TODO: shouldn't this be in akita-cli/api_schema?
type Service struct {
	ID   akid.ServiceID `json:"id"`
	Name string         `json:"name"`
}

type User = api_schema.UserResponse
