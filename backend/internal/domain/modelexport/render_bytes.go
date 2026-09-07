package modelexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func finalizeDocument(document any) (*RenderResult, error) {
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Pi document: %w", err)
	}
	return finalizeJSONBytes(raw, PiFileName), nil
}

// finalizeJSONBytes owns the delivered UTF-8 byte contract after each target's
// serialization: a single trailing newline, matching hash and download metadata.
func finalizeJSONBytes(raw []byte, fileName string) *RenderResult {
	content := string(raw) + "\n"
	sum := sha256.Sum256([]byte(content))
	return &RenderResult{
		Content: content, ContentSHA256: hex.EncodeToString(sum[:]),
		MIMEType: "application/json;charset=utf-8", FileName: fileName,
	}
}
