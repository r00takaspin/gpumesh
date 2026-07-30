package provider

import (
	"encoding/json"
	"testing"
)

func TestNormalizeMessagesFlattensContentArray(t *testing.T) {
	raw := json.RawMessage(`[
		{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":" there"}]}
	]`)
	msgs, err := normalizeMessagesForOllama(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0]["content"] != "hi there" {
		t.Fatalf("content=%v", msgs[0]["content"])
	}
}

func TestNormalizeMessagesParsesToolCallArgumentStrings(t *testing.T) {
	raw := json.RawMessage(`[
		{"role":"assistant","content":"","tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"run_commands","arguments":"{\"command\":\"ls -la\"}"}}
		]},
		{"role":"tool","tool_call_id":"call_1","content":"total 0"}
	]`)
	msgs, err := normalizeMessagesForOllama(raw)
	if err != nil {
		t.Fatal(err)
	}
	tcs := msgs[0]["tool_calls"].([]interface{})
	fn := tcs[0].(map[string]interface{})["function"].(map[string]interface{})
	args, ok := fn["arguments"].(map[string]interface{})
	if !ok {
		t.Fatalf("arguments type=%T value=%v", fn["arguments"], fn["arguments"])
	}
	if args["command"] != "ls -la" {
		t.Fatalf("command=%v", args["command"])
	}
}

func TestNormalizeMessagesKeepsObjectToolArguments(t *testing.T) {
	raw := json.RawMessage(`[
		{"role":"assistant","content":"","tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"f","arguments":{"x":1}}}
		]}
	]`)
	msgs, err := normalizeMessagesForOllama(raw)
	if err != nil {
		t.Fatal(err)
	}
	fn := msgs[0]["tool_calls"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})
	args := fn["arguments"].(map[string]interface{})
	if args["x"].(float64) != 1 {
		t.Fatalf("args=%v", args)
	}
}

func TestEnsureArgsObjectInvalidJSONKept(t *testing.T) {
	got := ensureArgsObject(`{"broken`)
	if got != `{"broken` {
		t.Fatalf("got %v", got)
	}
}
