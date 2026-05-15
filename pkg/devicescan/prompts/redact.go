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

// MaxStepHeadBytes is the upper bound on a single timeline step's
// TextHead (DeviceScanPromptStep). Matches the server-side validation
// rule in DESIGN.md ("≤512 B per step head").
const MaxStepHeadBytes = 512

// truncMarker is appended to a shortened text so admins can see at a
// glance that it was lossy.
const truncMarker = "…"

// TruncateContent shortens s to at most maxBytes while preserving valid
// UTF-8, returns the SHA-256 hex of the *original* (untruncated) input,
// and reports the original byte length. When the input is short enough,
// head == s and no marker is appended. When maxBytes <= 0 the input is
// considered too short to truncate and is returned as-is — callers
// should pass a positive cap.
//
// Truncation always lands on a UTF-8 rune boundary by walking the
// truncation point backwards until DecodeLastRune succeeds. The
// trailing "…" marker is included in the byte budget so the returned
// string never exceeds maxBytes.
func TruncateContent(s string, maxBytes int) (head string, fullBytes int64, hashHex string) {
	sum := sha256.Sum256([]byte(s))
	hashHex = hex.EncodeToString(sum[:])
	fullBytes = int64(len(s))

	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, fullBytes, hashHex
	}

	// Leave room for the trailing marker. The cut is on a UTF-8
	// boundary, walked back if the naive cut lands mid-rune.
	cut := max(maxBytes-len(truncMarker), 0)
	for cut > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:cut])
		if r != utf8.RuneError {
			break
		}
		cut--
	}
	return s[:cut] + truncMarker, fullBytes, hashHex
}

// TruncatePromptText is a thin wrapper around TruncateContent that pins
// the cap to MaxPromptTextBytes — the prompt-text-specific limit
// retained from M1.
func TruncatePromptText(s string) (truncated string, fullBytes int64, hashHex string) {
	return TruncateContent(s, MaxPromptTextBytes)
}
