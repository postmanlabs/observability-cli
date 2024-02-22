package rest

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
)

type PostmanMetaData struct {
	CollectionID string `json:"collection_id"`
	Environment  string `json:"environment,omitempty"`
}

// TODO: shouldn't this be in akita-cli/api_schema?
type Service struct {
	ID              akid.ServiceID  `json:"id"`
	Name            string          `json:"name"`
	PostmanMetaData PostmanMetaData `json:"postman_meta_data"`
}

type User = api_schema.UserResponse

type CreateServiceResponse struct {
	RequestID  akid.RequestID `json:"request_id"`
	ResourceID akid.ServiceID `json:"resource_id"`
}

type ErrorResponse struct {
	RequestID  akid.RequestID `json:"request_id"`
	Message    string         `json:"message"`
	ResourceID string         `json:"resource_id"`
}

type InsightsService struct {
	ID   akid.ServiceID `json:"service_id"`
	Name string         `json:"service_name"`
}

type PostmanUser struct {
	ID     int    `json:"id"`
	Email  string `json:"email"`
	TeamID int    `json:"team_id"`
}
