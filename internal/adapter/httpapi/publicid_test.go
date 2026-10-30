package httpapi

import (
	"testing"
)

func TestIDCodecRoundTrip(t *testing.T) {
	codec, err := NewIDCodec("BG2WJknpz7slOYihdvbarqtEgIA8VC1Pf63UmTwS5HLQFoKMZc04exX9NRjDyu", 10)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	seen := map[string]bool{}
	for _, id := range []uint64{1, 2, 3, 1000, 1 << 40} {
		public := codec.Encode(id)
		if len(public) < 10 || seen[public] {
			t.Fatalf("id %d encoded to %q, too short or duplicated", id, public)
		}
		seen[public] = true
		if decoded, err := codec.Decode("id", public); err != nil || decoded != id {
			t.Fatalf("decode(%q) = %d, %v; want %d", public, decoded, err, id)
		}
	}
}

func TestIDCodecRejectsForeignIDs(t *testing.T) {
	codec, _ := NewIDCodec("BG2WJknpz7slOYihdvbarqtEgIA8VC1Pf63UmTwS5HLQFoKMZc04exX9NRjDyu", 10)
	valid := codec.Encode(42)
	for _, public := range []string{"", "42", "not-an-id!", valid + "x", valid[1:]} {
		if _, err := codec.Decode("id", public); err == nil {
			t.Errorf("decode(%q) must fail", public)
		}
	}
}
