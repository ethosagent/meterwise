package contract

import "reflect"

// Health is the /healthz body.
type Health struct {
	Status string `json:"status"`
}

// Version is the /api/v1/version body.
type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// ErrorBody is the JSON body of every API error.
type ErrorBody struct {
	Code    Code     `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

// DTO is a named API type; the generators walk its fields by reflection.
type DTO struct {
	Name string
	Type reflect.Type
}

// DTOs lists every API type exported to the web twin and openapi.json, in a
// fixed order. A new DTO is added here, nowhere else.
func DTOs() []DTO {
	return []DTO{
		{"Health", reflect.TypeFor[Health]()},
		{"Version", reflect.TypeFor[Version]()},
		{"ErrorBody", reflect.TypeFor[ErrorBody]()},
	}
}
