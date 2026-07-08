import { zodResolver } from "@hookform/resolvers/zod"
import { type UseFormProps } from "react-hook-form"
import { z } from "zod"

export const rewriteProfileScopeSchema = z.object({
  profileId: z.literal(1),
  reason: z.string().min(1),
})

export type RewriteProfileScopeValues = z.infer<typeof rewriteProfileScopeSchema>

export function createRewriteProfileScopeFormOptions(
  defaultValues: RewriteProfileScopeValues,
): UseFormProps<RewriteProfileScopeValues> {
  return {
    defaultValues,
    resolver: zodResolver(rewriteProfileScopeSchema),
    mode: "onSubmit",
  }
}
