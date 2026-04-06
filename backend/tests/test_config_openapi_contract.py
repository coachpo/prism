from fastapi.routing import APIRoute

from app.main import app
from app.schemas.schemas import (
    ConfigExportResponse,
    ConfigVendorCatalogExportResponse,
)


def _get_get_route(path: str) -> APIRoute:
    for route in app.routes:
        if isinstance(route, APIRoute) and route.path == path and "GET" in route.methods:
            return route
    raise AssertionError(f"GET route not found for {path}")


def test_profile_config_export_route_exposes_config_export_response_in_openapi():
    profile_route = _get_get_route("/api/config/profile/export")
    vendor_route = _get_get_route("/api/config/vendors/export")
    openapi = app.openapi()
    profile_schema = (
        openapi["paths"]["/api/config/profile/export"]["get"]["responses"]["200"]
        ["content"]["application/json"]["schema"]
    )
    vendor_schema = (
        openapi["paths"]["/api/config/vendors/export"]["get"]["responses"]["200"]
        ["content"]["application/json"]["schema"]
    )
    export_component = openapi["components"]["schemas"]["ConfigExportResponse"]

    assert profile_route.response_model is ConfigExportResponse
    assert profile_schema == {"$ref": "#/components/schemas/ConfigExportResponse"}
    assert vendor_route.response_model is ConfigVendorCatalogExportResponse
    assert vendor_schema == {
        "$ref": "#/components/schemas/ConfigVendorCatalogExportResponse"
    }
    assert "secret_payload" in export_component["properties"]
    assert "secret_payload" in export_component["required"]
