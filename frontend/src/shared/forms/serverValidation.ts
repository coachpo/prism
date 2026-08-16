import { ApiError } from "@/lib/api"

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
  const message = cleanString(record.message) || cleanString(record.detail) || cleanString(record.error)
  if (!message) return null
  const field = cleanString(record.field) || cleanString(record.path) || cleanString(record.pointer) || "server"
  const code = cleanString(record.code)

  return { field, message, ...(code ? { code } : {}) }
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
  if (issue.code) {
    return `${issue.field} (${issue.code}): ${issue.message}`
  }
  return `${issue.field}: ${issue.message}`
}

export function extractServerValidation(error: unknown, fallback: string): ServerValidationResult {
  if (error instanceof ApiError) {
    const issues = collectIssues(error.detail)
    if (issues.length > 0) {
      return { issues, summary: issues.map(formatServerValidationIssue).join("\n") }
    }

    return { issues: [], summary: error.message || fallback }
  }

  return {
    issues: [],
    summary: error instanceof Error && error.message ? error.message : fallback,
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
