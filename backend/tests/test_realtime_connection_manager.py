from __future__ import annotations

import asyncio
import json
from typing import Any

from fastapi import WebSocket
from starlette.types import Message

from app.services.realtime.connection_manager import ConnectionManager


class WebSocketHarness:
    def __init__(self, *, fail_on_send: bool = False):
        self.accepted = False
        self.fail_on_send = fail_on_send
        self.sent_messages: list[Any] = []
        self._first_receive = True
        self.websocket = WebSocket(
            {
                "type": "websocket",
                "path": "/api/realtime/ws",
                "headers": [],
                "query_string": b"",
                "client": ("127.0.0.1", 1234),
                "server": ("testserver", 80),
                "scheme": "ws",
                "subprotocols": [],
            },
            self._receive,
            self._send,
        )

    async def _receive(self) -> Message:
        if self._first_receive:
            self._first_receive = False
            return {"type": "websocket.connect"}
        return {"type": "websocket.disconnect", "code": 1000}

    async def _send(self, message: Message) -> None:
        if message["type"] == "websocket.accept":
            self.accepted = True
            return

        if message["type"] != "websocket.send":
            return

        if self.fail_on_send:
            raise RuntimeError("send failed")

        text_payload = message.get("text")
        if isinstance(text_payload, str):
            self.sent_messages.append(json.loads(text_payload))
            return

        self.sent_messages.append(message)


def test_profile_switch_replaces_previous_room_membership() -> None:
    manager = ConnectionManager()
    websocket = WebSocketHarness()

    connection_id = asyncio.run(manager.connect(websocket.websocket))
    assert websocket.accepted is True

    assert asyncio.run(manager.subscribe(connection_id, 1, "dashboard")) is True
    assert asyncio.run(manager.subscribe(connection_id, 2, "dashboard")) is True

    connection = manager.get_connection(connection_id)
    assert connection is not None
    assert connection.profile_id == 2
    assert connection.channels == {"dashboard"}
    assert manager.rooms == {(2, "dashboard"): {connection_id}}


def test_unsubscribe_paths_clear_empty_rooms_and_profile_scope() -> None:
    manager = ConnectionManager()
    websocket = WebSocketHarness()

    connection_id = asyncio.run(manager.connect(websocket.websocket))
    assert asyncio.run(manager.subscribe(connection_id, 7, "dashboard")) is True
    assert asyncio.run(manager.subscribe(connection_id, 7, "activity")) is True

    assert asyncio.run(manager.unsubscribe_channel(connection_id, "dashboard")) is True
    connection = manager.get_connection(connection_id)
    assert connection is not None
    assert connection.profile_id == 7
    assert connection.channels == {"activity"}
    assert (7, "dashboard") not in manager.rooms
    assert manager.rooms == {(7, "activity"): {connection_id}}

    assert asyncio.run(manager.unsubscribe(connection_id)) is True
    connection = manager.get_connection(connection_id)
    assert connection is not None
    assert connection.profile_id is None
    assert connection.channels == set()
    assert manager.rooms == {}


def test_broadcast_prunes_failed_and_stale_connections() -> None:
    manager = ConnectionManager()
    healthy_websocket = WebSocketHarness()
    failing_websocket = WebSocketHarness(fail_on_send=True)

    healthy_id = asyncio.run(manager.connect(healthy_websocket.websocket))
    failing_id = asyncio.run(manager.connect(failing_websocket.websocket))

    assert asyncio.run(manager.subscribe(healthy_id, 3, "dashboard")) is True
    assert asyncio.run(manager.subscribe(failing_id, 3, "dashboard")) is True

    manager.rooms[(3, "dashboard")].add("stale-connection")

    delivered = asyncio.run(
        manager.broadcast_to_profile(
            3,
            "dashboard",
            {"type": "dashboard.update", "count": 1},
        )
    )

    assert delivered == 1
    assert healthy_websocket.sent_messages == [{"type": "dashboard.update", "count": 1}]
    assert failing_id not in manager.connections
    assert manager.rooms == {(3, "dashboard"): {healthy_id}}


def test_send_to_connection_drops_failed_socket() -> None:
    manager = ConnectionManager()
    websocket = WebSocketHarness(fail_on_send=True)

    connection_id = asyncio.run(manager.connect(websocket.websocket))
    assert asyncio.run(manager.subscribe(connection_id, 9, "dashboard")) is True

    sent = asyncio.run(
        manager.send_to_connection(connection_id, {"type": "dashboard.update"})
    )

    assert sent is False
    assert connection_id not in manager.connections
    assert manager.rooms == {}
