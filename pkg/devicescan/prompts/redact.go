package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"unicode/utf8"
)

// MaxPromptTextBytes is the upper bound on the truncated prompt text
// uploaded by the CLI. Matches the server-side validation rule in
// DESIGN.md.
const MaxPromptTextBytes = 2048

// truncMarker is appended to a shortened prompt text so admins can see
// at a glance that it was lossy.
const truncMarker = "…"

// TruncatePromptText shortens s to at most MaxPromptTextBytes while
// preserving valid UTF-8, returns the SHA-256 hex of the *original*
// (untruncated) input, and reports the original byte length. When the
// input is short enough, truncated == s and no marker is appended.
//
// Truncation always lands on a UTF-8 rune boundary by walking the
// truncation point backwards until DecodeLastRune succeeds. The
// trailing "…" marker is included in the byte budget so the returned
// string never exceeds MaxPromptTextBytes.
func TruncatePromptText(s string) (truncated string, fullBytes int64, hashHex string) {
	sum := sha256.Sum256([]byte(s))
	hashHex = hex.EncodeToString(sum[:])
	fullBytes = int64(len(s))

	if len(s) <= MaxPromptTextBytes {
		return s, fullBytes, hashHex
	}

	// Leave room for the trailing marker. The cut is on a UTF-8
	// boundary, walked back if the naive cut lands mid-rune.
	cut := max(MaxPromptTextBytes-len(truncMarker), 0)
	for cut > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:cut])
		if r != utf8.RuneError {
			break
		}
		cut--
	}
	return s[:cut] + truncMarker, fullBytes, hashHex
}
