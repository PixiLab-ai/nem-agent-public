package api

import (
	"encoding/json"
	"testing"
)

func TestDecode(t *testing.T) {
	var list struct {
		Companions []Companion `json:"companions"`
	}
	if err := decode(200, []byte(`{"companions":[{"uuid":"a","name":"Nyx"}]}`), &list); err != nil || len(list.Companions) != 1 {
		t.Fatalf("plain body: %v %v", err, list)
	}
	var raw json.RawMessage
	if err := decode(200, []byte(`{"result":"success","data":{"x":1}}`), &raw); err != nil || string(raw) != `{"x":1}` {
		t.Fatalf("envelope: %v %s", err, raw)
	}
	err := decode(401, []byte(`{"error":"api_key_expired"}`), nil)
	if e, ok := err.(*Error); !ok || e.Code != "api_key_expired" || !AuthFailed(err) {
		t.Fatalf("401: %v", err)
	}
	err = decode(200, []byte(`{"result":"error","message":"nope"}`), &raw)
	if e, ok := err.(*Error); !ok || e.Code != "nope" {
		t.Fatalf("envelope error: %v", err)
	}
	if err := decode(500, nil, nil); err == nil {
		t.Fatal("500 without a body passed")
	}
}

func TestChatURL(t *testing.T) {
	c := New("https://nemini.ai", "swm_x", "ua")
	u, err := c.ChatURL("c 1", "Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://nemini.ai/_special/rest/Pixi/api/swarm/companions/c%201/chat/ws?_=Europe%2FParis"
	if u != want {
		t.Fatalf("ChatURL = %s\n want %s", u, want)
	}
	c = New("http://localhost:8080", "swm_x", "ua")
	if u, _ := c.ChatURL("c", ""); u[:5] != "ws://" {
		t.Fatalf("dev site socket = %s", u)
	}
}
