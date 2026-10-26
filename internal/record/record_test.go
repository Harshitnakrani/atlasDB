package record

import (
	"bytes"
	"testing"

	"atlasDB/internal/common"
)

func TestRecordEncodeDecode(t *testing.T) {
	values := []common.Value{
		common.IntValue(42),
		common.TextValue("AtlasDB from scratch"),
		common.FloatValue(3.14159),
		common.BoolValue(true),
		common.BoolValue(false),
		common.NullValue(),
		common.BlobValue([]byte{0xDE, 0xAD, 0xBE, 0xEF}),
	}

	encoded, err := EncodeValues(values)
	if err != nil {
		t.Fatalf("EncodeValues failed: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded) != len(values) {
		t.Fatalf("Length mismatch: expected %d, got %d", len(values), len(decoded))
	}

	for i := range values {
		orig := values[i]
		dec := decoded[i]

		if orig.Type != dec.Type {
			t.Fatalf("Col %d type mismatch: expected %v, got %v", i, orig.Type, dec.Type)
		}
		if orig.Compare(dec) != 0 {
			t.Fatalf("Col %d value mismatch: expected %v, got %v", i, orig, dec)
		}
		if orig.Type == common.TypeBlob && !bytes.Equal(orig.Blob, dec.Blob) {
			t.Fatalf("Col %d blob mismatch", i)
		}
	}
}
