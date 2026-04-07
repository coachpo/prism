from __future__ import annotations

from collections.abc import Mapping
from typing import Any

SYSTEM_VENDOR_DEFINITIONS: tuple[dict[str, str], ...] = (
    {
        "key": "openai",
        "name": "OpenAI",
        "description": "OpenAI API (GPT models)",
        "icon_key": "openai",
    },
    {
        "key": "anthropic",
        "name": "Anthropic",
        "description": "Anthropic API (Claude models)",
        "icon_key": "anthropic",
    },
    {
        "key": "gemini",
        "name": "Gemini",
        "description": "Google Gemini API",
        "icon_key": "gemini",
    },
)

LEGACY_SYSTEM_VENDOR_ALIASES: dict[str, str] = {
    "google": "gemini",
}

SYSTEM_VENDOR_KEYS = frozenset(vendor["key"] for vendor in SYSTEM_VENDOR_DEFINITIONS)
READONLY_VENDOR_KEYS = frozenset(
    set(SYSTEM_VENDOR_KEYS) | set(LEGACY_SYSTEM_VENDOR_ALIASES)
)
SYSTEM_VENDOR_BY_KEY = {vendor["key"]: vendor for vendor in SYSTEM_VENDOR_DEFINITIONS}
VENDOR_IDENTITY_FIELDS = frozenset({"key", "name", "description", "icon_key"})


def is_readonly_vendor_key(key: str | None) -> bool:
    return isinstance(key, str) and key in READONLY_VENDOR_KEYS


def resolve_canonical_vendor_key(key: str | None) -> str | None:
    if not isinstance(key, str):
        return None
    return LEGACY_SYSTEM_VENDOR_ALIASES.get(key, key)


def get_canonical_system_vendor(key: str | None) -> dict[str, str] | None:
    canonical_key = resolve_canonical_vendor_key(key)
    if canonical_key is None:
        return None
    vendor = SYSTEM_VENDOR_BY_KEY.get(canonical_key)
    if vendor is None:
        return None
    return dict(vendor)


def apply_canonical_vendor_identity(
    vendor: Any, canonical_vendor: Mapping[str, str]
) -> bool:
    changed = False
    for field in VENDOR_IDENTITY_FIELDS:
        next_value = canonical_vendor[field]
        if getattr(vendor, field) != next_value:
            setattr(vendor, field, next_value)
            changed = True
    return changed


def has_identity_updates(update_data: Mapping[str, object]) -> bool:
    return any(field in update_data for field in VENDOR_IDENTITY_FIELDS)
