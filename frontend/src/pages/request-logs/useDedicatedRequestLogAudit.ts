import { useCallback, useEffect, useState } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import type { AuditLogDetail, AuditLogListItem } from "@/lib/types";
import type { RequestLogDetail } from "@/lib/types/request-logs";
import { deriveRequestLogAuditWindow } from "./requestLogAuditWindow";
import {
	resolveRequestAuditCaptureMode,
	type RequestAuditCaptureMode,
} from "./requestLogAuditState";

/**
 * Three independent read lanes (C3):
 *
 * - **request** — the owning request-log detail. Keyed by request ID alone, so
 *   paging the audit list (`cursor`) or switching the selected record never
 *   re-issues `requestDetail`.
 * - **list** — one page of audit records for this request at one cursor. Its
 *   failures and retries never touch the other two lanes.
 * - **detail** — the selected record's payload. Switching selection reloads
 *   only this lane.
 *
 * Every lane owns its own loading/error surface so a failure in one keeps the
 * others (and the sticky page context) on screen instead of flashing the whole
 * page back to a spinner.
 */

export type RequestLanePhase =
	| "idle"
	| "loading"
	| "ready"
	| "missing"
	| "disabled"
	| "invalid_timestamp"
	| "error";

export type ListLanePhase = "idle" | "loading" | "ready" | "empty" | "error";

export type DetailLanePhase =
	| "idle"
	| "loading"
	| "ready"
	/** The requested audit id is not on the loaded page. */
	| "missing_selection"
	| "error";

export interface RequestLaneState {
	phase: RequestLanePhase;
	request: RequestLogDetail | null;
	captureMode: RequestAuditCaptureMode | null;
	error: string | null;
}

export interface ListLaneState {
	phase: ListLanePhase;
	items: AuditLogListItem[];
	nextCursor: string | null;
	hasMore: boolean;
	error: string | null;
}

export interface DetailLaneState {
	phase: DetailLanePhase;
	detail: AuditLogDetail | null;
	selectedAuditId: number | null;
	/** The raw `audit_id` query value when it matched nothing on the page. */
	missingAuditLabel: string | null;
	error: string | null;
}

interface UseDedicatedRequestLogAuditParams {
	cursor?: string;
	requestId: string | null;
	selectedAuditId: number | null;
	selectedAuditParamPresent: boolean;
	selectedAuditParamLabel: string | null;
}

export interface DedicatedRequestLogAuditState {
	request: RequestLaneState;
	list: ListLaneState;
	detail: DetailLaneState;
	retryRequest: () => void;
	retryList: () => void;
	retryDetail: () => void;
}

const IDLE_REQUEST: RequestLaneState = {
	phase: "idle",
	request: null,
	captureMode: null,
	error: null,
};
const IDLE_LIST: ListLaneState = {
	phase: "idle",
	items: [],
	nextCursor: null,
	hasMore: false,
	error: null,
};
const IDLE_DETAIL: DetailLaneState = {
	phase: "idle",
	detail: null,
	selectedAuditId: null,
	missingAuditLabel: null,
	error: null,
};

function getErrorMessage(error: unknown, fallback: string): string {
	return error instanceof Error ? error.message : fallback;
}

function isRequestMissing(error: unknown): boolean {
	return error instanceof ApiError && error.status === 404;
}

function parseAuditWindow(
	request: RequestLogDetail,
): { from: string; to: string } | null {
	const summaryCreated = request.summary?.created_at;
	if (!summaryCreated) return null;
	try {
		return deriveRequestLogAuditWindow(summaryCreated);
	} catch {
		return null;
	}
}

export function useDedicatedRequestLogAudit({
	cursor,
	requestId,
	selectedAuditId,
	selectedAuditParamPresent,
	selectedAuditParamLabel,
}: UseDedicatedRequestLogAuditParams): DedicatedRequestLogAuditState {
	const messages = getStaticMessages();
	const [requestLane, setRequestLane] = useState<RequestLaneState>(IDLE_REQUEST);
	const [listLane, setListLane] = useState<ListLaneState>(IDLE_LIST);
	const [detailLane, setDetailLane] = useState<DetailLaneState>(IDLE_DETAIL);
	const [requestNonce, setRequestNonce] = useState(0);
	const [listNonce, setListNonce] = useState(0);
	const [detailNonce, setDetailNonce] = useState(0);
	const normalizedCursor = cursor?.trim() ?? "";

	// ------------------------------------------------------------------
	// Lane 1: request detail — keyed by request ID only.
	// ------------------------------------------------------------------
	useEffect(() => {
		if (requestId === null) {
			setRequestLane(IDLE_REQUEST);
			return;
		}
		let cancelled = false;
		setRequestLane({
			phase: "loading",
			request: null,
			captureMode: null,
			error: null,
		});
		void (async () => {
			try {
				const request = await api.stats.requestDetail(requestId);
				if (cancelled) return;
				const captureMode = resolveRequestAuditCaptureMode(request.routing);
				if (captureMode === "disabled") {
					setRequestLane({ phase: "disabled", request, captureMode, error: null });
					return;
				}
				// An unusable request timestamp blocks the whole audit lookup:
				// no window means no bounded list read may be issued.
				if (!parseAuditWindow(request)) {
					setRequestLane({
						phase: "invalid_timestamp",
						request,
						captureMode,
						error: null,
					});
					return;
				}
				setRequestLane({
					phase: "ready",
					request,
					captureMode,
					error: null,
				});
			} catch (error) {
				if (cancelled) return;
				if (isRequestMissing(error)) {
					setRequestLane({ ...IDLE_REQUEST, phase: "missing" });
					return;
				}
				setRequestLane({
					phase: "error",
					request: null,
					captureMode: null,
					error: getErrorMessage(error, messages.requestLogs.loadFailed),
				});
			}
		})();
		return () => {
			cancelled = true;
		};
	}, [messages.requestLogs.loadFailed, requestNonce, requestId]);

	// ------------------------------------------------------------------
	// Lane 2: audit record page — request-ready + cursor scoped.
	// ------------------------------------------------------------------
	const auditWindow =
		requestLane.phase === "ready" && requestLane.request !== null
			? parseAuditWindow(requestLane.request)
			: null;

	useEffect(() => {
		if (
			requestLane.phase !== "ready" ||
			requestId === null ||
			requestLane.captureMode === "disabled" ||
			!auditWindow
		) {
			setListLane(IDLE_LIST);
			return;
		}
		let cancelled = false;
		// A new cursor replaces the previous page; the previous page's rows are
		// withdrawn instead of masquerading as the target cursor's cohort.
		setListLane((current) => ({
			...current,
			phase: "loading",
			items: normalizedCursor ? [] : current.items,
			error: null,
		}));
		void (async () => {
			try {
				const page = await api.audit.listForRequestLog(requestId, {
					from: auditWindow.from,
					to: auditWindow.to,
					limit: 20,
					cursor: normalizedCursor || undefined,
				});
				if (cancelled) return;
				setListLane({
					phase: page.items.length === 0 ? "empty" : "ready",
					items: page.items,
					nextCursor: page.next_cursor,
					hasMore: page.has_more,
					error: null,
				});
			} catch (error) {
				if (cancelled) return;
				setListLane({
					phase: "error",
					items: [],
					nextCursor: null,
					hasMore: false,
					error: getErrorMessage(error, messages.requestLogs.auditListLoadFailed),
				});
			}
		})();
		return () => {
			cancelled = true;
		};
		// requestLane.captureMode/auditWindow derive from lane 1's committed read;
		// their identities change exactly when the request lane does.
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [
		requestId,
		requestLane.phase,
		requestLane.request,
		requestLane.captureMode,
		normalizedCursor,
		listNonce,
		messages.requestLogs.auditListLoadFailed,
	]);

	// ------------------------------------------------------------------
	// Lane 3: selected record detail — selection-scoped.
	// ------------------------------------------------------------------
	const resolvedSelection = (() => {
		if (listLane.phase !== "ready") return null;
		if (!selectedAuditParamPresent) return listLane.items[0] ?? null;
		if (selectedAuditId === null) return null;
		return listLane.items.find((item) => item.id === selectedAuditId) ?? null;
	})();

	useEffect(() => {
		if (listLane.phase === "loading") {
			setDetailLane((current) => ({ ...current, phase: "idle", detail: null }));
			return;
		}
		if (listLane.phase !== "ready") {
			setDetailLane(IDLE_DETAIL);
			return;
		}
		if (listLane.items.length === 0) {
			setDetailLane(IDLE_DETAIL);
			return;
		}
		if (!selectedAuditParamPresent) {
			if (!resolvedSelection) {
				setDetailLane(IDLE_DETAIL);
				return;
			}
		} else if (selectedAuditId !== null && !resolvedSelection) {
			setDetailLane({
				phase: "missing_selection",
				detail: null,
				selectedAuditId: null,
				missingAuditLabel: selectedAuditParamLabel,
				error: null,
			});
			return;
		} else if (!resolvedSelection) {
			setDetailLane(IDLE_DETAIL);
			return;
		}
		const selected = resolvedSelection;
		let cancelled = false;
		setDetailLane({
			phase: "loading",
			detail: null,
			selectedAuditId: selected.id,
			missingAuditLabel: null,
			error: null,
		});
		void (async () => {
			try {
				const detail = await api.audit.get(selected.id);
				if (cancelled) return;
				setDetailLane({
					phase: "ready",
					detail,
					selectedAuditId: selected.id,
					missingAuditLabel: null,
					error: null,
				});
			} catch (error) {
				if (cancelled) return;
				setDetailLane({
					phase: "error",
					detail: null,
					selectedAuditId: selected.id,
					missingAuditLabel: null,
					error: getErrorMessage(error, messages.requestLogs.auditDetailLoadFailed),
				});
			}
		})();
		return () => {
			cancelled = true;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [
		listLane.phase,
		listLane.items,
		resolvedSelection?.id,
		selectedAuditId,
		selectedAuditParamPresent,
		selectedAuditParamLabel,
		detailNonce,
		messages.requestLogs.auditDetailLoadFailed,
	]);

	const retryRequest = useCallback(() => {
		setRequestNonce((current) => current + 1);
	}, []);
	const retryList = useCallback(() => {
		setListNonce((current) => current + 1);
	}, []);
	const retryDetail = useCallback(() => {
		setDetailNonce((current) => current + 1);
	}, []);

	return {
		request: requestLane,
		list: listLane,
		detail: detailLane,
		retryRequest,
		retryList,
		retryDetail,
	};
}

export type { AuditLogDetail, AuditLogListItem };
