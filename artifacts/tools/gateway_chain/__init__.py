"""Live end-to-end suite for the Prism gateway request chain.

Boots the real stack with ./start.sh, sends real requests to real upstream
models, and verifies that every link of the chain recorded what it should.
"""

__all__ = ["ALL_CASES"]

from .cases_audit import CASES as AUDIT_CASES
from .cases_forward import CASES as FORWARD_CASES
from .cases_ingress import CASES as INGRESS_CASES
from .cases_launcher import CASES as LAUNCHER_CASES
from .cases_pipeline import CASES as PIPELINE_CASES
from .cases_readback import CASES as READBACK_CASES
from .cases_records import CASES as RECORD_CASES

ALL_CASES = [
    *LAUNCHER_CASES,
    *INGRESS_CASES,
    *FORWARD_CASES,
    *RECORD_CASES,
    *AUDIT_CASES,
    *READBACK_CASES,
    *PIPELINE_CASES,
]
