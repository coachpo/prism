"""Bring a freshly started instance to the state the case matrix assumes.

The database is restored from another instance, so nothing here may assume a
seeded proxy key or a working upstream credential. Whatever cannot be
established is reported, and the dependent cases become BLOCKED rather than
silently passing.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field

from .httpclient import Gateway
from .store import Store


@dataclass
class BootstrapResult:
    proxy_key: str | None = field(default=None, repr=False)
    proxy_key_name: str | None = None
    live_model: str | None = None
    dead_model: str | None = None
    failover_model: str | None = None
    upstream_endpoint_id: int | None = None
    notes: list[str] = field(default_factory=list)

    def to_json(self) -> dict:
        return {
            "proxy_key_name": self.proxy_key_name,
            "proxy_key_present": bool(self.proxy_key),
            "live_model": self.live_model,
            "dead_model": self.dead_model,
            "failover_model": self.failover_model,
            "upstream_endpoint_id": self.upstream_endpoint_id,
            "notes": self.notes,
        }


def ensure_proxy_key(gateway: Gateway, *, name_prefix: str = "gateway-chain") -> tuple[str | None, str | None, str]:
    """Create a fresh proxy key. The plaintext is returned exactly once."""
    name = f"{name_prefix}-{uuid.uuid4().hex[:8]}"
    created = gateway.management("POST", "/settings/auth/proxy-keys", body={"name": name})
    if created.status != 201:
        return None, None, f"proxy key creation failed: HTTP {created.status} {created.text[:200]}"
    payload = created.json() or {}
    key = payload.get("key")
    if not key:
        return None, name, "proxy key response carried no plaintext key"
    return key, name, ""


def delete_proxy_key(gateway: Gateway, name: str) -> None:
    listing = gateway.management("GET", "/settings/auth/proxy-keys")
    if listing.status != 200:
        return
    for item in (listing.json() or {}).get("items") or []:
        if item.get("name") == name and item.get("id") is not None:
            gateway.management("DELETE", f"/settings/auth/proxy-keys/{item['id']}")


def sweep_stale_proxy_keys(gateway: Gateway, *, name_prefix: str = "gateway-chain") -> list[str]:
    """Remove keys left behind by an interrupted run.

    A run that is killed never reaches its cleanup, so its key survives. Left
    unswept they accumulate against the 100-key capacity limit until key
    creation itself starts failing.
    """
    listing = gateway.management("GET", "/settings/auth/proxy-keys")
    if listing.status != 200:
        return []
    removed: list[str] = []
    for item in (listing.json() or {}).get("items") or []:
        name = item.get("name") or ""
        if name.startswith(name_prefix) and item.get("id") is not None:
            deleted = gateway.management("DELETE", f"/settings/auth/proxy-keys/{item['id']}")
            if deleted.status == 200:
                removed.append(name)
    return removed


def apply_upstream_credential(gateway: Gateway, endpoint_id: int, api_key: str) -> str:
    """Write a working upstream credential onto one endpoint of the local instance.

    Only the local instance is touched. The remote instance the config was
    synced from is never written back to.
    """
    updated = gateway.management("PUT", f"/endpoints/{endpoint_id}", body={"api_key": api_key})
    if updated.status != 200:
        return f"could not set the upstream key on endpoint {endpoint_id}: HTTP {updated.status} {updated.text[:200]}"
    return ""


def discover_models(
    store: Store,
    gateway: Gateway,
    *,
    live_model: str | None,
    live_endpoint_id: int | None = None,
    max_probes: int = 3,
    probe_timeout_s: float = 25.0,
) -> tuple[str | None, str | None, list[str]]:
    """Find a model whose upstream refuses, and one with several peers.

    Discovery is empirical: every candidate is probed with a one-token request,
    because the configuration alone cannot say which credentials still work.
    """
    notes: list[str] = []
    rows = store.json_rows(
        "select mc.model_id, count(mat.id) as target_count "
        "from model_configs mc "
        "left join model_access_targets mat on mat.source_model_config_id = mc.id and mat.is_enabled "
        "where mc.api_family = 'openai' and mc.is_enabled "
        "group by mc.model_id order by count(mat.id) desc, mc.model_id"
    )
    failover_model = None
    for row in rows:
        try:
            count = int(row.get("target_count") or 0)
        except (TypeError, ValueError):
            count = 0
        if count >= 2:
            failover_model = row.get("model_id")
            break
    if failover_model is None:
        notes.append("no caller model has two or more access targets; failover cannot be exercised")

    # A model is a good failure candidate when it has exactly one target and
    # that target does not share the endpoint the working credential was
    # installed on. Probing is capped so discovery cannot dominate the run.
    candidates = store.json_rows(
        "select mc.model_id, count(mat.id) as target_count "
        "from model_configs mc "
        "join model_access_targets mat on mat.source_model_config_id = mc.id and mat.is_enabled "
        "left join connections cn on cn.id = mat.target_connection_id "
        "where mc.api_family = 'openai' and mc.is_enabled "
        f"and coalesce(cn.endpoint_id, -1) <> {int(live_endpoint_id or -1)} "
        "group by mc.model_id having count(mat.id) = 1 order by mc.model_id"
    )
    dead_model = None
    probed: list[str] = []
    for row in candidates[:max_probes]:
        candidate = row.get("model_id")
        if not candidate or candidate == live_model:
            continue
        probed.append(candidate)
        probe = gateway.chat(candidate, "ping", max_tokens=1, timeout=probe_timeout_s)
        if probe.status in (401, 402, 403):
            dead_model = candidate
            break
    if dead_model is None:
        notes.append(
            "no model with a rejecting upstream was found among "
            f"{probed or 'no candidates'}; the relayed-failure cases will be blocked"
        )
    return dead_model, failover_model, notes


def run(store: Store, gateway: Gateway, *, env) -> BootstrapResult:
    result = BootstrapResult()

    swept = sweep_stale_proxy_keys(gateway)
    if swept:
        result.notes.append(f"swept {len(swept)} proxy key(s) left by an interrupted run")

    key, name, error = ensure_proxy_key(gateway)
    result.proxy_key, result.proxy_key_name = key, name
    if error:
        result.notes.append(error)
    gateway.proxy_key = key

    if env.upstream_api_key and env.upstream_endpoint_id:
        failure = apply_upstream_credential(gateway, env.upstream_endpoint_id, env.upstream_api_key)
        if failure:
            result.notes.append(failure)
        else:
            result.upstream_endpoint_id = env.upstream_endpoint_id
    elif env.upstream_api_key or env.upstream_endpoint_id:
        result.notes.append(
            "PRISM_CHAIN_UPSTREAM_API_KEY and PRISM_CHAIN_UPSTREAM_ENDPOINT_ID must both be set to install a credential"
        )

    if env.live_model:
        probe = gateway.chat(env.live_model, "ping", max_tokens=1, timeout=120.0)
        if probe.status == 200:
            result.live_model = env.live_model
        else:
            result.notes.append(
                f"configured live model {env.live_model} answered HTTP {probe.status}; "
                "live-upstream cases will be blocked"
            )
    else:
        result.notes.append("PRISM_CHAIN_LIVE_MODEL is unset; live-upstream cases will be blocked")

    dead_model, failover_model, notes = discover_models(
        store,
        gateway,
        live_model=result.live_model,
        live_endpoint_id=env.upstream_endpoint_id,
    )
    result.dead_model = dead_model
    result.failover_model = failover_model
    result.notes.extend(notes)
    return result
