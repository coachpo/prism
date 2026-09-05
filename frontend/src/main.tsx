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
          {/* 右上角会压住全局搜索、密度与主题按钮，还会盖到页头主动作的上缘；
              故障期间恰恰是这些控件最需要能点。 */}
          <Toaster position="bottom-right" />
        </TooltipProvider>
      </ThemeProvider>
    </LocaleProvider>
  </StrictMode>,
);
