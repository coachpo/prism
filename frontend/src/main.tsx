import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ThemeProvider } from "next-themes";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import {
  applyDensityMode,
  readDensityMode,
} from "@/components/layout/app-layout/densityMode";
import "./index.css";
import App from "./App.tsx";

// Before first paint, so a compact operator never sees a standard-density flash.
applyDensityMode(readDensityMode());

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <LocaleProvider>
      <ThemeProvider
        attribute="class"
        defaultTheme="system"
        enableSystem={true}
      >
        <TooltipProvider>
          <App />
          <Toaster position="top-right" />
        </TooltipProvider>
      </ThemeProvider>
    </LocaleProvider>
  </StrictMode>,
);
