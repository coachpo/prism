export type RewriteTableRow = {
  id: string
  label: string
  scope: "global" | "selected-profile" | "runtime"
}

type RewriteTableColumn = {
  accessorKey: keyof RewriteTableRow
  header: string
}

export const rewriteTableColumns: RewriteTableColumn[] = [
  {
    accessorKey: "label",
    header: "Label",
  },
  {
    accessorKey: "scope",
    header: "Scope",
  },
]
