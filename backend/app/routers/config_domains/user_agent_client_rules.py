from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.time import utc_now
from app.dependencies import get_db, get_effective_profile_id
from app.models.models import UserAgentClientRule
from app.schemas.schemas import (
    UserAgentClientRuleCreate,
    UserAgentClientRuleResponse,
    UserAgentClientRuleUpdate,
)

router = APIRouter()


@router.get(
    "/user-agent-client-rules",
    response_model=list[UserAgentClientRuleResponse],
)
async def list_user_agent_client_rules(
    db: Annotated[AsyncSession, Depends(get_db)],
    profile_id: Annotated[int, Depends(get_effective_profile_id)],
    include_disabled: bool = True,
):
    query = (
        select(UserAgentClientRule)
        .where(
            (UserAgentClientRule.is_system == True)  # noqa: E712
            | (UserAgentClientRule.profile_id == profile_id)
        )
        .order_by(
            UserAgentClientRule.is_system.desc(),
            UserAgentClientRule.id.asc(),
        )
    )
    if not include_disabled:
        query = query.where(UserAgentClientRule.enabled == True)  # noqa: E712
    return (await db.execute(query)).scalars().all()


@router.get(
    "/user-agent-client-rules/{rule_id}",
    response_model=UserAgentClientRuleResponse,
)
async def get_user_agent_client_rule(
    rule_id: int,
    db: Annotated[AsyncSession, Depends(get_db)],
    profile_id: Annotated[int, Depends(get_effective_profile_id)],
):
    rule = (
        await db.execute(
            select(UserAgentClientRule).where(
                UserAgentClientRule.id == rule_id,
                (UserAgentClientRule.is_system == True)  # noqa: E712
                | (UserAgentClientRule.profile_id == profile_id),
            )
        )
    ).scalar_one_or_none()
    if rule is None:
        raise HTTPException(status_code=404, detail="User agent client rule not found")
    return rule


@router.post(
    "/user-agent-client-rules",
    response_model=UserAgentClientRuleResponse,
    status_code=201,
)
async def create_user_agent_client_rule(
    body: UserAgentClientRuleCreate,
    db: Annotated[AsyncSession, Depends(get_db)],
    profile_id: Annotated[int, Depends(get_effective_profile_id)],
):
    rule = UserAgentClientRule(
        name=body.name,
        pattern=body.pattern,
        enabled=body.enabled,
        is_system=False,
        profile_id=profile_id,
    )
    db.add(rule)
    await db.flush()
    await db.refresh(rule)
    return rule


@router.patch(
    "/user-agent-client-rules/{rule_id}",
    response_model=UserAgentClientRuleResponse,
)
async def update_user_agent_client_rule(
    rule_id: int,
    body: UserAgentClientRuleUpdate,
    db: Annotated[AsyncSession, Depends(get_db)],
    profile_id: Annotated[int, Depends(get_effective_profile_id)],
):
    rule = (
        await db.execute(
            select(UserAgentClientRule).where(
                UserAgentClientRule.id == rule_id,
                (UserAgentClientRule.is_system == True)  # noqa: E712
                | (UserAgentClientRule.profile_id == profile_id),
            )
        )
    ).scalar_one_or_none()
    if rule is None:
        raise HTTPException(status_code=404, detail="User agent client rule not found")

    update_data = body.model_dump(exclude_unset=True)
    if rule.is_system:
        immutable_fields = {"name", "pattern"}
        attempted = immutable_fields & set(update_data)
        if attempted:
            raise HTTPException(
                status_code=400,
                detail=(
                    f"Cannot modify {', '.join(sorted(attempted))} on a system rule. "
                    "Only 'enabled' is mutable."
                ),
            )

    for field, value in update_data.items():
        setattr(rule, field, value)
    rule.updated_at = utc_now()

    await db.flush()
    await db.refresh(rule)
    return rule


@router.delete("/user-agent-client-rules/{rule_id}")
async def delete_user_agent_client_rule(
    rule_id: int,
    db: Annotated[AsyncSession, Depends(get_db)],
    profile_id: Annotated[int, Depends(get_effective_profile_id)],
):
    rule = (
        await db.execute(
            select(UserAgentClientRule).where(
                UserAgentClientRule.id == rule_id,
                UserAgentClientRule.profile_id == profile_id,
            )
        )
    ).scalar_one_or_none()
    if rule is None:
        raise HTTPException(status_code=404, detail="User agent client rule not found")
    if rule.is_system:
        raise HTTPException(
            status_code=400,
            detail="Cannot delete a system rule. Disable it instead.",
        )
    await db.delete(rule)
    await db.flush()
    return {"deleted": True}
