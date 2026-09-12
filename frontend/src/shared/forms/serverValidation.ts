import { ApiError } from "@/lib/api"
import { getStaticMessages } from "@/i18n/staticMessages"
import { managementErrorMessage } from "@/lib/api/errorMessage"

export type ServerValidationIssue = {
  code?: string
  field: string
  message: string
}

export type ServerValidationResult = {
  issues: ServerValidationIssue[]
  summary: string
}

function cleanString(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function issueFromRecord(record: Record<string, unknown>): ServerValidationIssue | null {
  if (!(cleanString(record.message) || cleanString(record.detail) || cleanString(record.error))) return null
  const field = cleanString(record.field) || cleanString(record.path) || cleanString(record.pointer) || "server"
  const code = cleanString(record.code)

  return { field, message: getStaticMessages().common.requestErrors.fieldInvalid, ...(code ? { code } : {}) }
}

function collectIssues(value: unknown): ServerValidationIssue[] {
  if (Array.isArray(value)) {
    return value.flatMap((item) => collectIssues(item))
  }
  if (!isRecord(value)) {
    return []
  }

  const issues: ServerValidationIssue[] = []
  const direct = issueFromRecord(value)
  if (direct) issues.push(direct)

  for (const key of ["routing_plan_issues", "errors", "issues", "validation_errors"] as const) {
    if (Array.isArray(value[key])) {
      issues.push(...collectIssues(value[key]))
    }
  }

  if (isRecord(value.detail)) {
    issues.push(...collectIssues(value.detail))
  }

  return dedupeIssues(issues)
}

function dedupeIssues(issues: ServerValidationIssue[]): ServerValidationIssue[] {
  const seen = new Set<string>()
  return issues.filter((issue) => {
    const key = `${issue.field}\u0000${issue.code ?? ""}\u0000${issue.message}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export function formatServerValidationIssue(issue: ServerValidationIssue): string {
  const copy = getStaticMessages().common.requestErrors
  const label = copy.fields[issue.field as keyof typeof copy.fields]
  return label ? `${label}：${copy.fieldInvalid}` : copy.invalid
}

export function extractServerValidation(error: unknown, fallback: string): ServerValidationResult {
  if (error instanceof ApiError) {
    const issues = [400, 409, 422].includes(error.status) ? collectIssues(error.detail).filter(issue => issue.field !== "server") : []
    if (issues.length > 0) {
      return { issues, summary: [...new Set(issues.map(formatServerValidationIssue))].join("\n") }
    }

    return { issues: [], summary: managementErrorMessage(error.status) }
  }

  return {
    issues: [],
    summary: fallback,
  }
}

export function fieldErrorsFromServerValidation<TField extends string>(
  validation: ServerValidationResult,
  fields: readonly TField[],
): Partial<Record<TField, string>> {
  const fieldSet = new Set<string>(fields)
  const errors: Partial<Record<TField, string>> = {}
  for (const issue of validation.issues) {
    if (fieldSet.has(issue.field) && !errors[issue.field as TField]) {
      errors[issue.field as TField] = issue.message
    }
  }
  return errors
}
