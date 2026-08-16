package safediag

import (
	"strings"
)

// RedactedMarker is the fixed replacement for any scrubbed value.
const RedactedMarker = "[REDACTED]"

// ScrubResult carries the scrubbed value and whether a replacement occurred.
type ScrubResult struct {
	Value     string
	Redacted  bool
	Truncated bool
}

// ScrubOptions controls value scrubbing.
type ScrubOptions struct {
	// MaxBytes caps the final value (UTF-8 code-point safe). Zero means no cap.
	MaxBytes int
}

// ScrubValue applies the fixed-bottom-line value scrubber to a caller or
// provider controlled string. It is used for correlation IDs, user agents,
// labels, paths, and extracted diagnostic text. It never leaks the original
// secret.
func ScrubValue(input string, options ScrubOptions) ScrubResult {
	if input == "" {
		return ScrubResult{Value: "", Redacted: false}
	}
	scrubbed, redacted := scrubValueInner(input)
	truncated := false
	if options.MaxBytes > 0 {
		safe, wasTruncated := TruncateUTF8(scrubbed, options.MaxBytes)
		scrubbed = safe
		truncated = wasTruncated
	}
	return ScrubResult{Value: scrubbed, Redacted: redacted, Truncated: truncated}
}

func scrubValueInner(input string) (string, bool) {
	redacted := false
	// 1. Replace Bearer/Basic credentials.
	output := replaceAuthSchemeCredentials(input, &redacted)
	// 2. Redact JWT-like fragments (three dot-separated base64url segments).
	output = redactJWTLikeFragments(output, &redacted)
	// 2b. Redact API-key-like fragments (sk-/pk-/rk-/AKIA-/AIza/xox patterns
	//     and long standalone token-like runs).
	output = redactAPIKeyLikeFragments(output, &redacted)
	// 3. Redact sensitive key=value and key: value fragments.
	output = redactSensitiveKeyValues(output, &redacted)
	// 4. Redact URL userinfo and query-string credentials inside the text.
	output = redactURLSecretsInText(output, &redacted)
	// 5. Remove control characters (newlines/tabs survive for later folding).
	output = stripControlCharacters(output)
	// 6. Fold repeated whitespace.
	output = foldWhitespace(output)
	return strings.TrimSpace(output), redacted
}

func replaceAuthSchemeCredentials(input string, redacted *bool) string {
	output := input
	lower := strings.ToLower(output)
	for _, scheme := range []string{"bearer ", "basic "} {
		searchFrom := 0
		for {
			idx := strings.Index(lower[searchFrom:], scheme)
			if idx < 0 {
				break
			}
			idx += searchFrom
			valueStart := idx + len(scheme)
			end := valueStart
			for end < len(output) && output[end] != ',' && output[end] != ';' && output[end] != ' ' && output[end] != '\n' && output[end] != '\t' && output[end] != '\r' {
				end++
			}
			output = output[:valueStart] + RedactedMarker + output[end:]
			*redacted = true
			// Continue searching after the replaced region; the scheme token
			// itself is not a credential and must not be re-matched forever.
			searchFrom = valueStart + len(RedactedMarker)
			lower = strings.ToLower(output)
		}
	}
	return output
}

func redactJWTLikeFragments(input string, redacted *bool) string {
	fields := strings.Fields(input)
	changed := false
	for i, field := range fields {
		if isJWTLike(field) {
			fields[i] = RedactedMarker
			changed = true
		}
	}
	if !changed {
		return input
	}
	*redacted = true
	return strings.Join(fields, " ")
}

func redactAPIKeyLikeFragments(input string, redacted *bool) string {
	fields := strings.Fields(input)
	changed := false
	for i, field := range fields {
		if isAPIKeyLike(field) {
			fields[i] = RedactedMarker
			changed = true
		}
	}
	if !changed {
		return input
	}
	*redacted = true
	return strings.Join(fields, " ")
}

func isAPIKeyLike(field string) bool {
	lower := strings.ToLower(field)
	// Known key prefixes: OpenAI sk-, Anthropic sk-ant-, Gemini AIza, AWS
	// AKIA, and generic xox (Slack) tokens.
	for _, prefix := range []string{"sk-ant-", "sk-", "pk-", "rk-", "ak-", "aiza", "akia", "xox"} {
		if strings.HasPrefix(lower, prefix) && len(field) > len(prefix) {
			return true
		}
	}
	// Long standalone token-like runs (>= 24 chars, base64/alnum with dashes
	// or underscores) are treated as credentials.
	if len(field) >= 24 {
		digits := 0
		tokenLike := true
		for _, c := range field {
			if c >= '0' && c <= '9' {
				digits++
				continue
			}
			if isBase64URLChar(c) {
				continue
			}
			tokenLike = false
			break
		}
		if tokenLike && digits > 0 {
			return true
		}
	}
	return false
}

func isJWTLike(field string) bool {
	parts := strings.Split(field, ".")
	if len(parts) != 3 {
		return false
	}
	// A real compact JWT has three substantial base64url segments (header,
	// payload, signature). Requiring each segment >= 8 chars and a total of
	// >= 60 chars prevents dotted identifiers like
	// "openai.responses.input_tokens" from being mistaken for JWTs.
	total := 0
	for _, part := range parts {
		if len(part) < 8 {
			return false
		}
		total += len(part)
		for _, c := range part {
			if !isBase64URLChar(c) {
				return false
			}
		}
	}
	return total >= 60
}

func isBase64URLChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

func redactSensitiveKeyValues(input string, redacted *bool) string {
	output := input
	matcher := NewSensitiveNameMatcher()
	// Iterate over each line and redact key=value / key: value where the key
	// is a sensitive name. A key is a bounded token of key characters.
	lines := strings.Split(output, "\n")
	for lineIndex, line := range lines {
		scanLine := line
		searchFrom := 0
		for {
			lower := strings.ToLower(scanLine)
			bestPos := -1
			for _, sep := range []byte{'=', ':'} {
				for idx := searchFrom; idx < len(lower); idx++ {
					sepIndex := strings.IndexByte(lower[idx:], sep)
					if sepIndex < 0 {
						break
					}
					pos := idx + sepIndex
					keyStart := pos
					for keyStart > 0 && isKeyChar(lower[keyStart-1]) {
						keyStart--
					}
					// A JSON-quoted key ("api_key":) has quotes as boundaries;
					// walk back over key chars inside the quotes too.
					if keyStart > 0 && lower[keyStart-1] == '"' {
						keyStart--
						for keyStart > 0 && isKeyChar(lower[keyStart-1]) {
							keyStart--
						}
						if keyStart > 0 && lower[keyStart-1] == '"' {
							keyStart--
						}
					}
					keyRaw := lower[keyStart:pos]
					key := strings.Trim(strings.TrimSpace(keyRaw), "\"")
					if key != "" && matcher.IsSensitiveName(key) && (bestPos < 0 || pos < bestPos) {
						bestPos = pos
					}
					idx = pos + 1
				}
			}
			if bestPos < 0 {
				break
			}
			end := bestPos + 1
			for end < len(scanLine) && scanLine[end] != ',' && scanLine[end] != ';' && scanLine[end] != '\t' {
				end++
			}
			value := scanLine[bestPos+1 : end]
			trimmedValue := strings.ToLower(strings.TrimSpace(value))
			// Skip matches whose value was already fully handled by a more
			// specific rule ("Authorization: Bearer [REDACTED]" or a marker at
			// the start); this also avoids re-matching the marker forever.
			if trimmedValue == strings.ToLower(RedactedMarker) ||
				strings.HasPrefix(trimmedValue, "bearer ") ||
				strings.HasPrefix(trimmedValue, "basic ") {
				searchFrom = end
				continue
			}
			scanLine = scanLine[:bestPos+1] + RedactedMarker + scanLine[end:]
			*redacted = true
			searchFrom = bestPos + 1 + len(RedactedMarker)
		}
		lines[lineIndex] = scanLine
	}
	return strings.Join(lines, "\n")
}

func isKeyChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
}

func redactURLSecretsInText(input string, redacted *bool) string {
	output := input
	searchFrom := 0
	for {
		lower := strings.ToLower(output)
		schemeIdx := -1
		for _, prefix := range []string{"https://", "http://"} {
			if idx := strings.Index(lower[searchFrom:], prefix); idx >= 0 {
				absIdx := idx + searchFrom
				if schemeIdx < 0 || absIdx < schemeIdx {
					schemeIdx = absIdx
				}
			}
		}
		if schemeIdx < 0 {
			return output
		}
		start := schemeIdx
		end := start
		for end < len(output) && !strings.ContainsRune(" \t\n\r,;", rune(output[end])) {
			end++
		}
		urlText := output[start:end]
		scrubbedURL := ScrubURLText(urlText)
		if scrubbedURL == urlText {
			// Nothing changed; advance past this URL to avoid a loop.
			searchFrom = end
			continue
		}
		output = output[:start] + scrubbedURL + output[end:]
		*redacted = true
		// The scrubbed URL may still contain the scheme prefix; advance past
		// the replaced region so it is not re-scrubbed forever.
		searchFrom = start + len(scrubbedURL)
	}
}

// ScrubURLText removes userinfo and redacts query-string values from a URL
// string, preserving scheme/host/path and query parameter names. Fragments
// are removed.
func ScrubURLText(rawURL string) string {
	queryIdx := strings.IndexByte(rawURL, '?')
	fragmentIdx := strings.IndexByte(rawURL, '#')
	end := len(rawURL)
	if queryIdx >= 0 {
		end = queryIdx
	} else if fragmentIdx >= 0 {
		end = fragmentIdx
	}
	base := rawURL[:end]
	rest := ""
	if queryIdx >= 0 {
		rest = rawURL[queryIdx:]
		if fragmentIdx >= 0 {
			rest = rawURL[queryIdx:fragmentIdx]
		}
	}
	// Remove userinfo: find the last '@' between the scheme end and the first
	// '/' after the scheme (the "://" separator must not count as a path).
	schemeEnd := strings.Index(base, "://")
	if schemeEnd < 0 {
		schemeEnd = -3
	}
	schemeEnd += 3
	userinfoIdx := -1
	for i := schemeEnd; i < len(base); i++ {
		if base[i] == '@' {
			userinfoIdx = i
		}
		if base[i] == '/' {
			break
		}
	}
	if userinfoIdx >= 0 {
		base = base[:schemeEnd] + RedactedMarker + "@" + base[userinfoIdx+1:]
	}
	if rest == "" {
		return base
	}
	// Redact query values, preserve names.
	query := strings.TrimPrefix(rest, "?")
	pairs := strings.Split(query, "&")
	for i, pair := range pairs {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		pairs[i] = pair[:eq] + "=" + RedactedMarker
	}
	return base + "?" + strings.Join(pairs, "&")
}

func stripControlCharacters(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	for _, r := range input {
		if r == '\n' || r == '\t' || r == '\r' {
			builder.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func foldWhitespace(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	space := false
	for _, r := range input {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			space = true
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteRune(r)
	}
	if space {
		builder.WriteByte(' ')
	}
	return builder.String()
}
