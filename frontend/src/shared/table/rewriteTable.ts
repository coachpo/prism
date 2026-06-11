import { type ColumnDef } from "@tanstack/react-table"

export type RewriteTableRow = {
  id: string
  label: string
  scope: "global" | "selected-profile" | "runtime"
}

export const rewriteTableColumns: ColumnDef<RewriteTableRow>[] = [
  {
    accessorKey: "label",
    header: "Label",
  },
  {
    accessorKey: "scope",
    header: "Scope",
  },
]
