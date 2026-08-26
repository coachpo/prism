import { describe, expect, it } from "vitest"

import {
  getNextOperationalSort,
  paginateOperationalRows,
  sortOperationalRows,
} from "./operationalTableState"

describe("operational table state", () => {
  it("sorts numeric strings with the existing locale-aware order", () => {
    const rows = [{ label: "item 10" }, { label: "item 2" }, { label: "item 1" }]

    expect(
      sortOperationalRows(
        rows,
        { column: "label", direction: "asc" },
        (row) => row.label,
        "en",
      ),
    ).toEqual([{ label: "item 1" }, { label: "item 2" }, { label: "item 10" }])
  })

  it("cycles the active sort column and direction", () => {
    expect(getNextOperationalSort({ column: "name", direction: "asc" }, "status")).toEqual({
      column: "status",
      direction: "asc",
    })
    expect(getNextOperationalSort({ column: "name", direction: "asc" }, "name")).toEqual({
      column: "name",
      direction: "desc",
    })
  })

  it("clamps page indexes and reports the visible slice", () => {
    expect(paginateOperationalRows([1, 2, 3, 4, 5], 1, 2)).toMatchObject({
      currentPageIndex: 1,
      endIndex: 4,
      hasNextPage: true,
      hasPreviousPage: true,
      pageRows: [3, 4],
      startIndex: 2,
      totalPages: 3,
    })
    expect(paginateOperationalRows([1, 2, 3, 4, 5], 99, 2).pageRows).toEqual([5])
  })
})
