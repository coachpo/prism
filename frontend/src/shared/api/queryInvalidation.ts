import type { QueryClient, QueryKey } from "@tanstack/react-query"
import { rewriteQueryKeys, type RewriteProfileId } from "./queryKeys"

export type RewriteMutationInvalidationScope =
  | { scope: "selected-profile"; profileId: RewriteProfileId }
  | { scope: "global" }
  | { scope: "runtime-bypass" }

export function getRewriteMutationInvalidationKeys(
  rule: RewriteMutationInvalidationScope,
): QueryKey[] {
  if (rule.scope === "selected-profile") {
    return [rewriteQueryKeys.selectedProfile(rule.profileId).all]
  }

  if (rule.scope === "global") {
    return [rewriteQueryKeys.global.all]
  }

  return [rewriteQueryKeys.runtimeBypass.all]
}

export async function invalidateRewriteMutationScope(
  queryClient: QueryClient,
  rule: RewriteMutationInvalidationScope,
) {
  await Promise.all(
    getRewriteMutationInvalidationKeys(rule).map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  )
}
