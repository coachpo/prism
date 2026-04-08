from __future__ import annotations

import importlib
from pathlib import Path
from typing import Any, cast

from sqlalchemy import Column, MetaData, String, Table, inspect, text
from sqlalchemy.engine import Connection

command = cast(Any, importlib.import_module("alembic.command"))
Config = cast(Any, getattr(importlib.import_module("alembic.config"), "Config"))

ALEMBIC_VERSION_TABLE_NAME = "alembic_version"
ALEMBIC_VERSION_NUM_LENGTH = 128


def build_alembic_version_table(
    metadata: MetaData,
    *,
    table_name: str = ALEMBIC_VERSION_TABLE_NAME,
    schema: str | None = None,
) -> Table:
    return Table(
        table_name,
        metadata,
        Column(
            "version_num",
            String(ALEMBIC_VERSION_NUM_LENGTH),
            nullable=False,
            primary_key=True,
        ),
        schema=schema,
    )


def _quote_identifier(identifier: str) -> str:
    escaped_identifier = identifier.replace('"', '""')
    return f'"{escaped_identifier}"'


def _qualify_table_name(table_name: str, schema: str | None) -> str:
    if schema is None:
        return _quote_identifier(table_name)
    return f"{_quote_identifier(schema)}.{_quote_identifier(table_name)}"


def ensure_alembic_version_table_capacity(
    connection: Connection,
    *,
    table_name: str = ALEMBIC_VERSION_TABLE_NAME,
    schema: str | None = None,
) -> None:
    inspector = inspect(connection)
    if not inspector.has_table(table_name, schema=schema):
        metadata = MetaData()
        version_table = build_alembic_version_table(
            metadata,
            table_name=table_name,
            schema=schema,
        )
        metadata.create_all(connection, tables=[version_table])
        return

    version_column = next(
        (
            column
            for column in inspector.get_columns(table_name, schema=schema)
            if column["name"] == "version_num"
        ),
        None,
    )
    if version_column is None:
        raise RuntimeError(f"{table_name}.version_num column is missing")

    current_length = getattr(version_column["type"], "length", None)
    if current_length is None or current_length >= ALEMBIC_VERSION_NUM_LENGTH:
        return

    qualified_table_name = _qualify_table_name(table_name, schema)
    connection.execute(
        text(
            f"ALTER TABLE {qualified_table_name} "
            f"ALTER COLUMN version_num TYPE VARCHAR({ALEMBIC_VERSION_NUM_LENGTH})"
        )
    )


def _build_alembic_config(database_url: str) -> Any:
    package_root = Path(__file__).resolve().parents[1]
    config = Config(str(package_root / "alembic.ini"))
    migrations_dir = package_root / "alembic"
    config.set_main_option("script_location", str(migrations_dir))
    config.set_main_option("sqlalchemy.url", database_url.replace("%", "%%"))
    return config


def run_migrations(database_url: str) -> None:
    config = _build_alembic_config(database_url)
    command.upgrade(config, "head")
