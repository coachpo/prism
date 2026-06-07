package runtime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const overflowAffinityCacheTTL = 10 * time.Minute

const (
	overflowAffinityHeaderName               = "x-session-affinity"
	overflowAffinityParentHeaderName         = "x-parent-session-id"
	overflowAffinityMaxHeaderBytes           = 256
	overflowAffinityHashPrefixLength         = 16
	overflowAffinityContextBucketSizeTokens  = 16 * 1024
	overflowAffinityUnknownContextBucket     = "unknown"
	overflowAffinityNoTerminalTargetSentinel = "none"
)

type overflowAffinityCache struct {
	mu      sync.RWMutex
	entries map[overflowAffinityCacheKey]overflowAffinityCacheEntry
	now     func() time.Time
}

type overflowAffinityCacheKey struct {
	profileID                      int
	operationName                  string
	sourceResolvedModelID          string
	sourceSelectedTerminalTargetID string
	configuredPromotionTargetID    string
	affinityDigest                 string
	routingGenerationToken         string
	contextBucket                  string
}

type overflowAffinityCacheEntry struct {
	promotionTargetID              string
	sourceModelID                  string
	sourceSelectedTerminalTargetID string
	generationToken                string
	contextBucket                  string
	affinityHashPrefix             string
	parentHashPrefix               *string
	expiresAt                      time.Time
}

type overflowAffinityCacheKeyInput struct {
	profileID                   int
	operationName               string
	sourceResolvedModelID       string
	sourceSelectedTerminalID    *int
	configuredPromotionTargetID string
	affinityHeaders             http.Header
	processLocalSecret          string
	routingGenerationToken      string
	contextBucket               string
}

type overflowAffinityCacheEntryInput struct {
	promotionTargetID        string
	sourceModelID            string
	sourceSelectedTerminalID *int
	generationToken          string
	contextBucket            string
}

type overflowAffinityHeaderMaterial struct {
	affinityDigest     string
	affinityHashPrefix string
	parentHashPrefix   *string
}

func newOverflowAffinityCache(now func() time.Time) *overflowAffinityCache {
	if now == nil {
		now = time.Now
	}
	return &overflowAffinityCache{
		entries: make(map[overflowAffinityCacheKey]overflowAffinityCacheEntry),
		now:     now,
	}
}

func (cache *overflowAffinityCache) get(key overflowAffinityCacheKey) (overflowAffinityCacheEntry, bool) {
	if cache == nil {
		return overflowAffinityCacheEntry{}, false
	}
	referenceNow := cache.nowUTC()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok {
		return overflowAffinityCacheEntry{}, false
	}
	if !entry.expiresAt.After(referenceNow) {
		delete(cache.entries, key)
		return overflowAffinityCacheEntry{}, false
	}
	return entry, true
}

func (cache *overflowAffinityCache) put(key overflowAffinityCacheKey, entry overflowAffinityCacheEntry) {
	if cache == nil {
		return
	}
	entry.expiresAt = cache.nowUTC().Add(overflowAffinityCacheTTL)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[overflowAffinityCacheKey]overflowAffinityCacheEntry)
	}
	cache.entries[key] = entry
}

func (cache *overflowAffinityCache) pruneExpired(referenceNow time.Time) {
	if cache == nil {
		return
	}
	referenceNow = referenceNow.UTC()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key, entry := range cache.entries {
		if !entry.expiresAt.After(referenceNow) {
			delete(cache.entries, key)
		}
	}
}

func (cache *overflowAffinityCache) sizeForTest() int {
	if cache == nil {
		return 0
	}
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return len(cache.entries)
}

func (cache *overflowAffinityCache) nowUTC() time.Time {
	if cache.now == nil {
		cache.now = time.Now
	}
	return cache.now().UTC()
}

func buildOverflowAffinityCacheKey(input overflowAffinityCacheKeyInput) (overflowAffinityCacheKey, overflowAffinityHeaderMaterial, bool) {
	material, ok := buildOverflowAffinityHeaderMaterial(input.profileID, input.affinityHeaders, input.processLocalSecret)
	if !ok {
		return overflowAffinityCacheKey{}, overflowAffinityHeaderMaterial{}, false
	}
	contextBucket := input.contextBucket
	if contextBucket == "" {
		contextBucket = overflowAffinityUnknownContextBucket
	}
	key := overflowAffinityCacheKey{
		profileID:                      input.profileID,
		operationName:                  input.operationName,
		sourceResolvedModelID:          input.sourceResolvedModelID,
		sourceSelectedTerminalTargetID: overflowAffinityTerminalTargetKey(input.sourceSelectedTerminalID),
		configuredPromotionTargetID:    input.configuredPromotionTargetID,
		affinityDigest:                 material.affinityDigest,
		routingGenerationToken:         input.routingGenerationToken,
		contextBucket:                  contextBucket,
	}
	return key, material, true
}

func buildOverflowAffinityCacheEntry(input overflowAffinityCacheEntryInput, material overflowAffinityHeaderMaterial) overflowAffinityCacheEntry {
	contextBucket := input.contextBucket
	if contextBucket == "" {
		contextBucket = overflowAffinityUnknownContextBucket
	}
	return overflowAffinityCacheEntry{
		promotionTargetID:              input.promotionTargetID,
		sourceModelID:                  input.sourceModelID,
		sourceSelectedTerminalTargetID: overflowAffinityTerminalTargetKey(input.sourceSelectedTerminalID),
		generationToken:                input.generationToken,
		contextBucket:                  contextBucket,
		affinityHashPrefix:             material.affinityHashPrefix,
		parentHashPrefix:               material.parentHashPrefix,
	}
}

func buildOverflowAffinityHeaderMaterial(profileID int, headers http.Header, processLocalSecret string) (overflowAffinityHeaderMaterial, bool) {
	affinityValue, ok := normalizeOverflowAffinityHeaderValue(headers, overflowAffinityHeaderName)
	if !ok {
		return overflowAffinityHeaderMaterial{}, false
	}
	affinityDigest := hashOverflowAffinityValue(profileID, affinityValue, processLocalSecret)
	material := overflowAffinityHeaderMaterial{
		affinityDigest:     affinityDigest,
		affinityHashPrefix: overflowAffinityHashPrefix(affinityDigest),
	}
	if parentValue, parentOK := normalizeOverflowAffinityHeaderValue(headers, overflowAffinityParentHeaderName); parentOK {
		parentDigest := hashOverflowAffinityValue(profileID, parentValue, processLocalSecret)
		parentPrefix := overflowAffinityHashPrefix(parentDigest)
		material.parentHashPrefix = &parentPrefix
	}
	return material, true
}

func normalizeOverflowAffinityHeaderValue(headers http.Header, headerName string) (string, bool) {
	values := headers.Values(headerName)
	var selected string
	found := false
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if len([]byte(trimmed)) > overflowAffinityMaxHeaderBytes {
			return "", false
		}
		if !found {
			selected = trimmed
			found = true
			continue
		}
		if trimmed != selected {
			return "", false
		}
	}
	return selected, found
}

func hashOverflowAffinityValue(profileID int, normalizedValue string, processLocalSecret string) string {
	material := strconv.Itoa(profileID) + ":" + normalizedValue
	secret := strings.TrimSpace(processLocalSecret)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(material))
		return hex.EncodeToString(mac.Sum(nil))
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func overflowAffinityHashPrefix(digest string) string {
	if len(digest) <= overflowAffinityHashPrefixLength {
		return digest
	}
	return digest[:overflowAffinityHashPrefixLength]
}

func overflowAffinityContextBucket(estimation *requestContextEstimation) string {
	if estimation == nil || estimation.EstimatedTotalContextTokens < 0 {
		return overflowAffinityUnknownContextBucket
	}
	bucketStart := (estimation.EstimatedTotalContextTokens / overflowAffinityContextBucketSizeTokens) * overflowAffinityContextBucketSizeTokens
	bucketEnd := bucketStart + overflowAffinityContextBucketSizeTokens - 1
	return strconv.Itoa(bucketStart) + "-" + strconv.Itoa(bucketEnd)
}

func overflowAffinityTerminalTargetKey(selectedTerminalTargetID *int) string {
	if selectedTerminalTargetID == nil {
		return overflowAffinityNoTerminalTargetSentinel
	}
	return strconv.Itoa(*selectedTerminalTargetID)
}
