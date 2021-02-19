package rest

import (
	"github.com/akitasoftware/akita-libs/akid"
)

type Service struct {
	ID   akid.ServiceID `json:"id"`
	Name string         `json:"name"`
}
