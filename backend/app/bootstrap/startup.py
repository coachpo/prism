import asyncio
import logging
import os

import httpx
from sqlalchemy import func, or_, select, update

from app.core import database as database_core
from app.core.config import ensure_postgresql_database_url, get_settings
from app.core.crypto import encrypt_secret
from app.core.migrations import run_migrations
from app.models.models import (
    AppAuthSettings,
    Endpoint,
    HeaderBlocklistRule,
    ModelConfig,
    Profile,
    UsageRequestEvent,
    UserAgentClientRule,
    UserSetting,
    Vendor,
)
from app.services.profile_invariants import ensure_profile_invariants
from app.vendor_catalog import (
    SYSTEM_VENDOR_DEFINITIONS,
    apply_canonical_vendor_identity,
    get_canonical_system_vendor,
)

logger = logging.getLogger(__name__)

SKIP_STARTUP_SEQUENCE_ENV = "PRISM_SKIP_STARTUP_SEQUENCE"

DEFAULT_VENDORS = [dict(vendor) for vendor in SYSTEM_VENDOR_DEFINITIONS]

SYSTEM_BLOCKLIST_DEFAULTS: list[dict[str, str]] = [
    {"name": "Cloudflare headers", "match_type": "prefix", "pattern": "cf-"},
    {"name": "Cloudflare extended headers", "match_type": "prefix", "pattern": "x-cf-"},
    {
        "name": "Cloudflare Access headers",
        "match_type": "prefix",
        "pattern": "cf-access-",
    },
    {"name": "B3 tracing headers", "match_type": "prefix", "pattern": "x-b3-"},
    {
        "name": "Datadog tracing headers",
        "match_type": "prefix",
        "pattern": "x-datadog-",
    },
    {"name": "CDN loop detection", "match_type": "exact", "pattern": "cdn-loop"},
    {"name": "Forwarded header", "match_type": "exact", "pattern": "forwarded"},
    {"name": "Via header", "match_type": "exact", "pattern": "via"},
    {"name": "X-Forwarded-For", "match_type": "exact", "pattern": "x-forwarded-for"},
    {"name": "X-Forwarded-Host", "match_type": "exact", "pattern": "x-forwarded-host"},
    {"name": "X-Forwarded-Port", "match_type": "exact", "pattern": "x-forwarded-port"},
    {
        "name": "X-Forwarded-Proto",
        "match_type": "exact",
        "pattern": "x-forwarded-proto",
    },
    {"name": "X-Real-IP", "match_type": "exact", "pattern": "x-real-ip"},
    {"name": "True-Client-IP", "match_type": "exact", "pattern": "true-client-ip"},
    {"name": "W3C Traceparent", "match_type": "exact", "pattern": "traceparent"},
    {"name": "W3C Tracestate", "match_type": "exact", "pattern": "tracestate"},
    {"name": "W3C Baggage", "match_type": "exact", "pattern": "baggage"},
    {"name": "X-Request-ID", "match_type": "exact", "pattern": "x-request-id"},
    {"name": "X-Correlation-ID", "match_type": "exact", "pattern": "x-correlation-id"},
    {"name": "AWS X-Ray trace", "match_type": "exact", "pattern": "x-amzn-trace-id"},
    {
        "name": "GCP Cloud Trace",
        "match_type": "exact",
        "pattern": "x-cloud-trace-context",
    },
]

SYSTEM_USER_AGENT_CLIENT_RULE_DEFAULTS: list[dict[str, str]] = [
    {"name": "Opencode", "pattern": "opencode"},
    {"name": "Codex", "pattern": "codex"},
    {"name": "Claude Code", "pattern": "claude(?:\\s|-)?(?:code|cli)"},
    {"name": "Gemini", "pattern": "gemini"},
    {"name": "Python", "pattern": "python"},
    {"name": "Curl", "pattern": "curl"},
]


async def seed_vendors() -> None:
    async with database_core.AsyncSessionLocal() as session:
        existing_vendors = (
            (await session.execute(select(Vendor).order_by(Vendor.id.asc())))
            .scalars()
            .all()
        )
        changed = False
        created_count = 0

        existing_by_key = {vendor.key: vendor for vendor in existing_vendors}
        legacy_google_vendor = existing_by_key.get("google")
        gemini_vendor = existing_by_key.get("gemini")

        if legacy_google_vendor is not None:
            canonical_gemini = get_canonical_system_vendor("gemini")
            if canonical_gemini is None:
                raise RuntimeError("Canonical gemini vendor definition is missing")

            if gemini_vendor is not None and gemini_vendor is not legacy_google_vendor:
                changed = (
                    apply_canonical_vendor_identity(gemini_vendor, canonical_gemini)
                    or changed
                )
                await session.execute(
                    update(ModelConfig)
                    .where(ModelConfig.vendor_id == legacy_google_vendor.id)
                    .values(vendor_id=gemini_vendor.id)
                )
                await session.delete(legacy_google_vendor)
                existing_vendors = [
                    vendor
                    for vendor in existing_vendors
                    if vendor is not legacy_google_vendor
                ]
                changed = True
            else:
                changed = (
                    apply_canonical_vendor_identity(
                        legacy_google_vendor, canonical_gemini
                    )
                    or changed
                )

        existing_by_key = {vendor.key: vendor for vendor in existing_vendors}

        for vendor_data in DEFAULT_VENDORS:
            existing_vendor = existing_by_key.get(vendor_data["key"])
            if existing_vendor is not None:
                changed = (
                    apply_canonical_vendor_identity(existing_vendor, vendor_data)
                    or changed
                )
                continue
            session.add(Vendor(**vendor_data))
            created_count += 1
            changed = True

        if changed:
            await session.commit()
            logger.info(
                "Ensured canonical system vendor catalog (created=%d)",
                created_count,
            )


async def seed_profile_invariants() -> None:
    async with database_core.AsyncSessionLocal() as session:
        _ = await ensure_profile_invariants(session)
        await session.commit()
        logger.info("Ensured default profile invariants")


async def seed_header_blocklist_rules() -> None:
    async with database_core.AsyncSessionLocal() as session:
        for default_rule in SYSTEM_BLOCKLIST_DEFAULTS:
            existing = (
                await session.execute(
                    select(HeaderBlocklistRule).where(
                        HeaderBlocklistRule.match_type == default_rule["match_type"],
                        HeaderBlocklistRule.pattern == default_rule["pattern"],
                        HeaderBlocklistRule.is_system == True,  # noqa: E712
                    )
                )
            ).scalar_one_or_none()
            if existing is not None:
                continue
            session.add(
                HeaderBlocklistRule(
                    name=default_rule["name"],
                    match_type=default_rule["match_type"],
                    pattern=default_rule["pattern"],
                    enabled=True,
                    is_system=True,
                )
            )
        await session.commit()
        logger.info("Seeded system header blocklist rules")


async def seed_user_agent_client_rules() -> None:
    async with database_core.AsyncSessionLocal() as session:
        changed = False
        existing_rules = list(
            (
                await session.execute(
                    select(UserAgentClientRule)
                    .where(UserAgentClientRule.is_system == True)  # noqa: E712
                    .order_by(UserAgentClientRule.id.asc())
                )
            )
            .scalars()
            .all()
        )
        for default_rule in SYSTEM_USER_AGENT_CLIENT_RULE_DEFAULTS:
            existing = next(
                (
                    rule
                    for rule in existing_rules
                    if rule.name == default_rule["name"]
                    or rule.pattern == default_rule["pattern"]
                ),
                None,
            )
            if existing is not None:
                if (
                    existing.name != default_rule["name"]
                    or existing.pattern != default_rule["pattern"]
                ):
                    existing.name = default_rule["name"]
                    existing.pattern = default_rule["pattern"]
                    changed = True
                continue
            new_rule = UserAgentClientRule(
                name=default_rule["name"],
                pattern=default_rule["pattern"],
                enabled=True,
                is_system=True,
            )
            session.add(new_rule)
            existing_rules.append(new_rule)
            changed = True

        if changed:
            await session.commit()
            logger.info("Seeded system user-agent client rules")


async def seed_user_settings() -> None:
    async with database_core.AsyncSessionLocal() as session:
        profile_ids = (
            (
                await session.execute(
                    select(Profile.id)
                    .where(Profile.deleted_at.is_(None))
                    .order_by(Profile.id.asc())
                )
            )
            .scalars()
            .all()
        )
        if not profile_ids:
            return

        existing_settings = list(
            (
                await session.execute(
                    select(UserSetting).where(UserSetting.profile_id.in_(profile_ids))
                )
            )
            .scalars()
            .all()
        )
        existing_profile_ids: set[int] = set()
        for settings in existing_settings:
            if hasattr(settings, "profile_id"):
                existing_profile_ids.add(settings.profile_id)
            elif isinstance(settings, int) and not isinstance(settings, bool):
                existing_profile_ids.add(settings)
        missing_profile_ids = [
            profile_id
            for profile_id in profile_ids
            if profile_id not in existing_profile_ids
        ]
        for profile_id in missing_profile_ids:
            session.add(
                UserSetting(
                    profile_id=profile_id,
                    report_currency_code="USD",
                    report_currency_symbol="$",
                    timezone_preference=None,
                )
            )

        if missing_profile_ids:
            await session.commit()
            logger.info(
                "Seeded default user settings for %d profile(s)",
                len(missing_profile_ids),
            )


async def seed_app_auth_settings() -> None:
    async with database_core.AsyncSessionLocal() as session:
        existing = (
            await session.execute(
                select(AppAuthSettings)
                .where(AppAuthSettings.singleton_key == "app")
                .limit(1)
            )
        ).scalar_one_or_none()
        if existing is None:
            session.add(AppAuthSettings(singleton_key="app", auth_enabled=False))
            await session.commit()
            logger.info("Seeded application auth settings")


async def encrypt_endpoint_secrets() -> None:
    async with database_core.AsyncSessionLocal() as session:
        endpoints = (
            (await session.execute(select(Endpoint).order_by(Endpoint.id.asc())))
            .scalars()
            .all()
        )
        updated_count = 0
        for endpoint in endpoints:
            encrypted = encrypt_secret(endpoint.api_key)
            if encrypted == endpoint.api_key:
                continue
            endpoint.api_key = encrypted
            updated_count += 1
        if updated_count > 0:
            await session.commit()
            logger.info("Encrypted endpoint secrets for %d endpoint(s)", updated_count)


async def run_startup_migrations() -> None:
    settings = get_settings()
    ensure_postgresql_database_url(settings.database_url)
    await asyncio.to_thread(run_migrations, settings.database_url)
    logger.info("Applied database migrations")


async def reconcile_usage_request_event_billing_fields() -> None:
    from app.services.stats_service import backfill_usage_request_event_billing_fields

    async with database_core.AsyncSessionLocal() as session:
        pending_usage_event_count = int(
            (
                await session.scalar(
                    select(func.count())
                    .select_from(UsageRequestEvent)
                    .where(
                        or_(
                            UsageRequestEvent.billable_flag.is_(None),
                            UsageRequestEvent.priced_flag.is_(None),
                        )
                    )
                )
            )
            or 0
        )
        if pending_usage_event_count == 0:
            return

        reconciliation = await backfill_usage_request_event_billing_fields(session)

    logger.info(
        "Reconciled UsageRequestEvent billing fields for %d pending row(s) "
        "(matched=%d unmatched=%d duplicate_candidates=%d)",
        pending_usage_event_count,
        reconciliation["matched_request_log_count"],
        reconciliation["unmatched_usage_event_count"],
        reconciliation["duplicate_candidate_count"],
    )


async def run_startup_sequence() -> None:
    if os.getenv(SKIP_STARTUP_SEQUENCE_ENV) == "1":
        logger.info("Skipping startup bootstrap; launcher already applied it")
        return

    await run_startup_migrations()
    await reconcile_usage_request_event_billing_fields()
    await seed_vendors()
    await seed_profile_invariants()
    await seed_user_settings()
    await seed_user_agent_client_rules()
    await seed_app_auth_settings()
    await encrypt_endpoint_secrets()
    await seed_header_blocklist_rules()


def build_http_client() -> httpx.AsyncClient:
    settings = get_settings()
    return httpx.AsyncClient(
        timeout=httpx.Timeout(
            connect=settings.connect_timeout,
            read=settings.read_idle_timeout,
            write=settings.write_timeout,
            pool=settings.pool_timeout,
        ),
        limits=httpx.Limits(max_connections=20),
        follow_redirects=True,
    )


__all__ = [
    "DEFAULT_VENDORS",
    "SKIP_STARTUP_SEQUENCE_ENV",
    "SYSTEM_BLOCKLIST_DEFAULTS",
    "build_http_client",
    "encrypt_endpoint_secrets",
    "reconcile_usage_request_event_billing_fields",
    "run_startup_migrations",
    "run_startup_sequence",
    "seed_app_auth_settings",
    "seed_header_blocklist_rules",
    "seed_profile_invariants",
    "seed_user_agent_client_rules",
    "seed_vendors",
    "seed_user_settings",
]
