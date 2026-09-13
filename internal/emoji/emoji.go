package emoji

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed ids.json
var idsJSON []byte

var ids map[string]string

func init() {
	if err := json.Unmarshal(idsJSON, &ids); err != nil {
		panic("emoji: bad ids.json: " + err.Error())
	}
}

const DefaultFallback = "🪙"

func ID(tradeID string) (string, bool) {
	id, ok := ids[tradeID]
	return id, ok
}

func Tag(tradeID string) string {
	id, ok := ids[tradeID]
	if !ok {
		return DefaultFallback
	}
	return fmt.Sprintf(`<tg-emoji emoji-id=%q>%s</tg-emoji>`, id, DefaultFallback)
}

func Len() int {
	return len(ids)
}
