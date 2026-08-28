package block

import (
	"encoding/json"
	"testing"
)

func TestDetectRoundTrip(t *testing.T) {
	env := Envelope{Content: []Item{
		Text("hello"),
		Image("image/png", []byte{0x89, 0x50, 0x4e, 0x47}),
	}}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := DetectJSON(raw)
	if !ok || len(got.Content) != 2 {
		t.Fatalf("detect = %+v,%v", got, ok)
	}
	if got.Content[0].Type != "text" || got.Content[0].Text != "hello" {
		t.Fatalf("text block = %+v", got.Content[0])
	}
	if got.Content[1].MIMEType != "image/png" || got.Content[1].Data != "iVBORw==" {
		t.Fatalf("image block = %+v", got.Content[1])
	}
}

func TestDetectRejectsNonEnvelopes(t *testing.T) {
	reject := []string{
		`{"content":[{"type":"text"}],"other":1}`,
		`{"content":[{"type":"text"}]}`,
		`{"content":[{"type":"audio","data":"x"}]}`,
		`{"content":[{"type":"image","mimeType":"image/png"}]}`,
		`{"content":"not-an-array"}`,
		`[{"content":[]}]`,
		`{"name":"plain"}`,
	}
	for _, s := range reject {
		if _, ok := DetectJSON([]byte(s)); ok {
			t.Fatalf("should NOT be envelope: %s", s)
		}
	}
}
