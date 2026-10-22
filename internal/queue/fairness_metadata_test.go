package queue

import (
	"encoding/json"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestQueueIdentityJSONCompatibility(t *testing.T) {
	message := &model.Message{ID: "identified", Tenant: "alice@example.test", TrafficClass: "transactional"}
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.Message
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Tenant != message.Tenant || decoded.TrafficClass != message.TrafficClass {
		t.Fatalf("queue identity = %+v", decoded)
	}
	decoded = model.Message{}
	if err := json.Unmarshal([]byte(`{"id":"legacy"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Tenant != "" || decoded.TrafficClass != "" {
		t.Fatalf("legacy metadata = %+v", decoded)
	}
}
