# ruff: noqa: F401
from fastapi import APIRouter

from app.routers.config_domains.blocklist import (
    create_header_blocklist_rule,
    delete_header_blocklist_rule,
    get_header_blocklist_rule,
    list_header_blocklist_rules,
    router as _blocklist_router,
    update_header_blocklist_rule,
)
from app.routers.config_domains.user_agent_client_rules import (
    create_user_agent_client_rule,
    delete_user_agent_client_rule,
    get_user_agent_client_rule,
    list_user_agent_client_rules,
    router as _user_agent_client_rules_router,
    update_user_agent_client_rule,
)
from app.routers.config_domains.import_export import (
    export_profile_config,
    export_vendor_catalog,
    import_profile_config,
    import_vendor_catalog,
    preview_profile_import,
    preview_vendor_catalog_import,
    router as _import_export_router,
    _validate_import,
)

router = APIRouter(prefix="/api/config", tags=["config"])
router.include_router(_import_export_router)
router.include_router(_blocklist_router)
router.include_router(_user_agent_client_rules_router)

export_config = export_profile_config
import_config = import_profile_config
