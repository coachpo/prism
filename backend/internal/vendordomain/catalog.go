package vendordomain

import "strings"

type VendorDefinition struct {
	Key         string
	Name        string
	Description string
	IconKey     string
}

var SystemVendorDefinitions = []VendorDefinition{
	{
		Key:         "openai",
		Name:        "OpenAI",
		Description: "OpenAI API (GPT models)",
		IconKey:     "openai",
	},
	{
		Key:         "anthropic",
		Name:        "Anthropic",
		Description: "Anthropic API (Claude models)",
		IconKey:     "anthropic",
	},
	{
		Key:         "gemini",
		Name:        "Gemini",
		Description: "Google Gemini API",
		IconKey:     "gemini",
	},
}

var LegacySystemVendorAliases = map[string]string{
	"google": "gemini",
}

var systemVendorByKey = func() map[string]VendorDefinition {
	items := make(map[string]VendorDefinition, len(SystemVendorDefinitions))
	for _, definition := range SystemVendorDefinitions {
		items[definition.Key] = definition
	}
	return items
}()

var readonlyVendorKeys = func() map[string]struct{} {
	items := make(map[string]struct{}, len(SystemVendorDefinitions)+len(LegacySystemVendorAliases))
	for _, definition := range SystemVendorDefinitions {
		items[definition.Key] = struct{}{}
	}
	for key := range LegacySystemVendorAliases {
		items[key] = struct{}{}
	}
	return items
}()

func IsReadonlyVendorKey(key string) bool {
	_, ok := readonlyVendorKeys[strings.TrimSpace(strings.ToLower(key))]
	return ok
}

func CanonicalSystemVendor(key string) (VendorDefinition, bool) {
	canonicalKey := strings.TrimSpace(strings.ToLower(key))
	if alias, ok := LegacySystemVendorAliases[canonicalKey]; ok {
		canonicalKey = alias
	}
	definition, ok := systemVendorByKey[canonicalKey]
	return definition, ok
}
