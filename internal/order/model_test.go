package order

import (
	"encoding/json"
	"testing"
)

func TestOrderJSONIncludesZeroTotal(t *testing.T) {
	encoded, err := json.Marshal(Order{Total: 0})
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal order JSON: %v", err)
	}
	value, ok := fields["total"]
	if !ok {
		t.Fatalf("JSON %s does not contain total", encoded)
	}
	if string(value) != "0" {
		t.Errorf("total = %s, want 0", value)
	}
}
