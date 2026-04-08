from __future__ import annotations

import importlib
from pathlib import Path
from typing import Any, cast

from sqlalchemy import MetaData

from app.core.migrations import (
    ALEMBIC_VERSION_NUM_LENGTH,
    build_alembic_version_table,
)

Config = cast(Any, getattr(importlib.import_module("alembic.config"), "Config"))
ScriptDirectory = cast(
    Any, getattr(importlib.import_module("alembic.script"), "ScriptDirectory")
)


def test_alembic_graph_has_one_head() -> None:
    backend_root = Path(__file__).resolve().parent.parent
    config = Config(str(backend_root / "alembic.ini"))
    config.set_main_option("script_location", str(backend_root / "app" / "alembic"))

    heads = ScriptDirectory.from_config(config).get_heads()

    assert heads == ["0018_connection_probe_interval_default"]


def test_alembic_version_table_length_covers_all_revision_ids() -> None:
    backend_root = Path(__file__).resolve().parent.parent
    config = Config(str(backend_root / "alembic.ini"))
    config.set_main_option("script_location", str(backend_root / "app" / "alembic"))

    revision_ids = [
        script.revision
        for script in ScriptDirectory.from_config(config).walk_revisions()
    ]

    assert (
        max(len(revision_id) for revision_id in revision_ids)
        <= ALEMBIC_VERSION_NUM_LENGTH
    )


def test_build_alembic_version_table_uses_wide_version_num_column() -> None:
    version_table = build_alembic_version_table(MetaData())

    assert (
        getattr(version_table.c.version_num.type, "length", None)
        == ALEMBIC_VERSION_NUM_LENGTH
    )
    assert version_table.c.version_num.primary_key is True
