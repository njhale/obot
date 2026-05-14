package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncatePromptText_ShortInputUnchanged(t *testing.T) {
	in := "hello world"
	got, full, hash := TruncatePromptText(in)
	if got != in {
		t.Errorf("short input mutated: got %q want %q", got, in)
	}
	if full != int64(len(in)) {
		t.Errorf("fullBytes: got %d want %d", full, len(in))
	}
	if !isHexLen(hash, 64) {
		t.Errorf("hash %q not 64 hex chars", hash)
	}
	sum := sha256.Sum256([]byte(in))
	if hash != hex.EncodeToString(sum[:]) {
		t.Errorf("hash mismatch")
	}
}

func TestTruncatePromptText_ExactLengthUnchanged(t *testing.T) {
	in := strings.Repeat("a", MaxPromptTextBytes)
	got, full, _ := TruncatePromptText(in)
	if got != in {
		t.Errorf("exact-length input was modified")
	}
	if full != int64(MaxPromptTextBytes) {
		t.Errorf("fullBytes: got %d want %d", full, MaxPromptTextBytes)
	}
	if !strings.HasSuffix(got, "a") {
		t.Errorf("no truncation marker should be appended at exact length")
	}
}

func TestTruncatePromptText_OneOverGetsMarker(t *testing.T) {
	in := strings.Repeat("a", MaxPromptTextBytes+1)
	got, full, _ := TruncatePromptText(in)
	if full != int64(MaxPromptTextBytes+1) {
		t.Errorf("fullBytes: got %d want %d", full, MaxPromptTextBytes+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated output should end with %q, got tail %q", "…", got[len(got)-6:])
	}
	if len(got) > MaxPromptTextBytes {
		t.Errorf("truncated len %d exceeds cap %d", len(got), MaxPromptTextBytes)
	}
}

func TestTruncatePromptText_UTF8BoundarySafe(t *testing.T) {
	// 4-byte rune (😀, U+1F600) repeated until well over the cap.
	rune4 := "😀"
	if utf8.RuneLen('😀') != 4 {
		t.Fatalf("test assumption: 😀 should be 4 bytes, got %d", utf8.RuneLen('😀'))
	}
	in := strings.Repeat(rune4, (MaxPromptTextBytes/4)+5)
	got, _, _ := TruncatePromptText(in)
	if !utf8.ValidString(got) {
		t.Errorf("truncated output is not valid UTF-8")
	}
	if len(got) > MaxPromptTextBytes {
		t.Errorf("truncated len %d exceeds cap %d", len(got), MaxPromptTextBytes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("missing truncation marker")
	}
}

func TestTruncatePromptText_HashOfFullText(t *testing.T) {
	in := strings.Repeat("z", MaxPromptTextBytes*2)
	_, _, hash := TruncatePromptText(in)
	sum := sha256.Sum256([]byte(in))
	if hash != hex.EncodeToString(sum[:]) {
		t.Errorf("hash should be over full untruncated text")
	}
}

func TestTruncatePromptText_StableHash(t *testing.T) {
	in := "deterministic input"
	_, _, h1 := TruncatePromptText(in)
	_, _, h2 := TruncatePromptText(in)
	if h1 != h2 {
		t.Errorf("hash unstable: %q vs %q", h1, h2)
	}
}

func TestTruncatePromptText_EmptyInput(t *testing.T) {
	got, full, hash := TruncatePromptText("")
	if got != "" {
		t.Errorf("empty input: got %q", got)
	}
	if full != 0 {
		t.Errorf("empty input fullBytes: got %d", full)
	}
	// Empty string's sha-256 is well-known.
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != emptySHA256 {
		t.Errorf("empty input hash: got %q want %q", hash, emptySHA256)
	}
}

func isHexLen(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
