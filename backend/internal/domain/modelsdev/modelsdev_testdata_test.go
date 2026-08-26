package modelsdev

import (
	"testing"
	"time"
)

const fixtureCatalog = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-test": {
        "id": "gpt-test",
        "name": "GPT Test",
        "description": "fixture model",
        "family": "gpt-test",
        "attachment": false,
        "reasoning": true,
        "tool_call": true,
        "structured_output": true,
        "temperature": true,
        "knowledge": "2025-05",
        "release_date": "2026-01-15",
        "last_updated": "2026-02-20",
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "open_weights": false,
        "limit": {"context": 128000, "input": 100000, "output": 16384},
        "cost": {"input": 2.50, "output": 10, "cache_read": 0, "cache_write": 1.25}
      },
      "gpt-long": {
        "id": "gpt-long",
        "name": "GPT Long",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "limit": {"context": 400000, "output": 32768},
        "cost": {
          "input": 30, "output": 180,
          "tiers": [{"input": 60, "output": 270, "tier": {"type": "context", "size": 272000}}],
          "context_over_200k": {"input": 60, "output": 270}
        }
      },
      "gpt-tiered-cache": {
        "id": "gpt-tiered-cache",
        "name": "GPT Tiered Cache",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "cost": {
          "input": 4, "output": 20, "cache_read": 0.4, "cache_write": 5,
          "tiers": [{"input": 8, "output": 30, "cache_read": 0.8, "cache_write": 10, "tier": {"type": "context", "size": 272000}}]
        }
      },
      "gpt-audio": {
        "id": "gpt-audio",
        "name": "GPT Audio",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "cost": {"input": 5, "output": 20, "input_audio": 10, "output_audio": 40}
      }
    }
  },
  "anthropic": {
    "id": "anthropic",
    "name": "Anthropic",
    "models": {
      "claude-test": {
        "id": "claude-test",
        "name": "Claude Test",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": false,
        "cost": {"input": 3, "output": 15, "cache_write": 3.75}
      },
      "shared-model": {
        "id": "shared-model",
        "name": "Shared Model",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": true,
        "cost": {"input": 1, "output": 2}
      }
    }
  },
  "azure": {
    "id": "azure",
    "name": "Azure",
    "models": {
      "shared-model": {
        "id": "shared-model",
        "name": "Shared Model on Azure",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": false,
        "cost": {"input": 1, "output": 2}
      }
    }
  },
  "google": {
    "id": "google",
    "name": "Google",
    "models": {
      "gemini-test": {
        "id": "gemini-test",
        "name": "Gemini Test",
        "release_date": "2026-02",
        "last_updated": "2026-02",
        "open_weights": true,
        "status": "deprecated",
        "cost": {"input": 1.25, "output": 10}
      }
    }
  }
}`

func loadFixtureCatalog(t *testing.T) *Catalog {
	t.Helper()
	providers, err := parseCatalog([]byte(fixtureCatalog))
	if err != nil {
		t.Fatalf("fixture catalog must validate: %v", err)
	}
	return &Catalog{ETag: `"fixture"`, FetchedAt: time.Unix(1700000000, 0).UTC(), Providers: providers}
}
