import { useMemo, useState, type ReactNode } from "react";

import {
  BreadcrumbEntitySetterContext,
  BreadcrumbEntityValueContext,
} from "./breadcrumbEntity";

export function BreadcrumbEntityProvider({ children }: { children: ReactNode }) {
  const [label, setLabel] = useState<string | null>(null);
  const setter = useMemo(() => setLabel, []);

  return (
    <BreadcrumbEntitySetterContext.Provider value={setter}>
      <BreadcrumbEntityValueContext.Provider value={label}>
        {children}
      </BreadcrumbEntityValueContext.Provider>
    </BreadcrumbEntitySetterContext.Provider>
  );
}
