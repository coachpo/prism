import { zodResolver } from "@hookform/resolvers/zod"
import type { ModelFormData } from "@/pages/models/modelFormState"
import { DEFAULT_MODEL_FORM_DATA } from "@/pages/models/modelFormState"
import { modelAuthoringSchema } from "./modelSchemas"

export function createModelAuthoringFormOptions(defaultValues: ModelFormData = DEFAULT_MODEL_FORM_DATA) {
  return {
    defaultValues,
    resolver: zodResolver(modelAuthoringSchema),
    mode: "onSubmit" as const,
  }
}
