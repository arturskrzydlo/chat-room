package server

import (
	"fmt"
	"os"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

var (
	schemaLoader    gojsonschema.JSONLoader
	onceSchemaLoad  sync.Once
	schemaLoadError error
)

func loadSchema() error {
	onceSchemaLoad.Do(func() {
		schemaBytes, err := os.ReadFile("internal/server/message_schema.json")
		if err != nil {
			schemaLoadError = fmt.Errorf("failed to load WebSocket JSON schema: %w", err)
			return
		}
		schemaLoader = gojsonschema.NewBytesLoader(schemaBytes)
	})
	return schemaLoadError
}

// ValidateWebSocketMessage validates a raw JSON message against the WebSocket schema
func ValidateWebSocketMessage(rawJSON []byte) ([]gojsonschema.ResultError, error) {
	if err := loadSchema(); err != nil {
		return nil, err
	}
	documentLoader := gojsonschema.NewBytesLoader(rawJSON)
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("validating webSocket message: %w", err)
	}
	if !result.Valid() {
		return result.Errors(), nil
	}
	return nil, nil
}
