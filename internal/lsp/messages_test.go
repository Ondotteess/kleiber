package lsp

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestServerCapabilities_TextDocumentSyncMode_DecodesSupportedShapes(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		want    TextDocumentSyncKind
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "missing",
			want:   TextDocumentSyncNone,
			wantOK: false,
		},
		{
			name:   "null",
			raw:    json.RawMessage("null"),
			want:   TextDocumentSyncNone,
			wantOK: false,
		},
		{
			name:   "integer kind",
			raw:    json.RawMessage("1"),
			want:   TextDocumentSyncFull,
			wantOK: true,
		},
		{
			name:   "options object",
			raw:    json.RawMessage(`{"openClose":true,"change":2}`),
			want:   TextDocumentSyncIncremental,
			wantOK: true,
		},
		{
			name:    "invalid shape",
			raw:     json.RawMessage(`["bad"]`),
			want:    TextDocumentSyncNone,
			wantOK:  false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := (ServerCapabilities{TextDocumentSync: tt.raw}).TextDocumentSyncMode()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && errors.Unwrap(err) == nil {
				t.Fatalf("err = %v, want wrapped decode error", err)
			}
			if got != tt.want {
				t.Errorf("mode = %d, want %d", got, tt.want)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}
