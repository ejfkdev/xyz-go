// Package block defines the reserved content-block envelope (xyz-spec
// §12.7): a result value whose JSON is an object with the single key
// "content", holding items of exactly the shapes
//
//	{"type": "text", "text": "..."}
//	{"type": "image", "mimeType": "...", "data": "<base64>"}
//
// Handlers may return block.Envelope directly, or any equivalent
// shape — the frontends detect the envelope duck-typed, so the reserve is
// not tied to this package. Binary payloads are base64 on the wire, never
// bare bytes; use block.Image to construct them.
package block

import (
	"encoding/base64"
	"encoding/json"
)

// Item is one block in the envelope. Use the Text/Image constructors; the
// struct is exported for direct literal use by handlers.
type Item struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// Envelope is the reserved block envelope shape.
type Envelope struct {
	Content []Item `json:"content"`
}

// Text constructs a text block.
func Text(s string) Item { return Item{Type: "text", Text: s} }

// Image constructs an image block; data is base64-encoded on the wire.
func Image(mimeType string, data []byte) Item {
	return Item{Type: "image", MIMEType: mimeType, Data: base64.StdEncoding.EncodeToString(data)}
}

// DetectJSON reports whether raw is exactly the reserved envelope and, if
// so, returns the decoded envelope. Anything else — extra top-level keys,
// item shapes beyond text/image, unknown types — is not the envelope.
func DetectJSON(raw []byte) (*Envelope, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || len(top) != 1 {
		return nil, false
	}
	blob, ok := top["content"]
	if !ok {
		return nil, false
	}
	var arr []json.RawMessage
	if json.Unmarshal(blob, &arr) != nil {
		return nil, false
	}
	env := &Envelope{Content: make([]Item, 0, len(arr))}
	for _, entry := range arr {
		var fields map[string]json.RawMessage
		if json.Unmarshal(entry, &fields) != nil {
			return nil, false
		}
		switch {
		case len(fields) == 2 && fields["type"] != nil && fields["text"] != nil:
			var typ, text string
			if json.Unmarshal(fields["type"], &typ) != nil || typ != "text" ||
				json.Unmarshal(fields["text"], &text) != nil {
				return nil, false
			}
			env.Content = append(env.Content, Item{Type: "text", Text: text})
		case len(fields) == 3 && fields["type"] != nil && fields["mimeType"] != nil && fields["data"] != nil:
			var typ, mime, data string
			if json.Unmarshal(fields["type"], &typ) != nil || typ != "image" ||
				json.Unmarshal(fields["mimeType"], &mime) != nil || mime == "" ||
				json.Unmarshal(fields["data"], &data) != nil || data == "" {
				return nil, false
			}
			env.Content = append(env.Content, Item{Type: "image", MIMEType: mime, Data: data})
		default:
			return nil, false
		}
	}
	return env, true
}
