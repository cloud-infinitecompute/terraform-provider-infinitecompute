// Package client is the public API client, generated from the contract
// api/openapi-v1.json (the API's /api/public/v1/openapi.json). Regenerate with
// `go generate ./...`.
package client

//go:generate go run ../../tools/openapi30 ../../api/openapi-v1.json openapi30.json
//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi30.json
