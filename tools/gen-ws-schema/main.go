package main

import (
	"encoding/json"
	"os"

	"github.com/invopop/jsonschema"

	"github.com/arturskrzydlo/chat-room/internal/messages"
)

func main() {
	r := new(jsonschema.Reflector)
	r.ExpandedStruct = true

	schema := r.Reflect(&messages.WsMessage{})

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(schema)
}
