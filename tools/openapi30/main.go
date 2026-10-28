// Command openapi30 rewrites the public API's OpenAPI 3.1 document (generated
// by FastAPI) as 3.0 for oapi-codegen: `anyOf: [X, {type: null}]` becomes X
// with `nullable: true`, and the version string changes. Nothing else.
//
//	go run ./tools/openapi30 api/openapi-v1.json internal/client/openapi30.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func rewrite(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if alts, ok := t["anyOf"].([]any); ok {
			var rest []any
			nullable := false
			for _, a := range alts {
				if m, ok := a.(map[string]any); ok && m["type"] == "null" {
					nullable = true
					continue
				}
				rest = append(rest, a)
			}
			if nullable {
				delete(t, "anyOf")
				if len(rest) == 1 {
					for k, val := range rest[0].(map[string]any) {
						if _, exists := t[k]; !exists {
							t[k] = val
						}
					}
				} else {
					t["anyOf"] = rest
				}
				t["nullable"] = true
			}
		}
		for k, val := range t {
			t[k] = rewrite(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = rewrite(t[i])
		}
		return t
	}
	return v
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: openapi30 <in 3.1 json> <out 3.0 json>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		panic(err)
	}
	doc = rewrite(doc).(map[string]any)
	doc["openapi"] = "3.0.3"
	out, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(os.Args[2], append(out, '\n'), 0o644); err != nil {
		panic(err)
	}
}
