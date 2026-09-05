import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { CornerDownLeft, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { getSharedModels } from "@/lib/referenceData";
import type { ModelConfigListItem } from "@/lib/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import { SHELL_SIDEBAR_GROUP_ORDER, type LocalizedShellSidebarItem } from "./useShellNavigation";

type Entry = {
  description?: string;
  icon: LocalizedShellSidebarItem["icon"] | typeof CornerDownLeft;
  key: string;
  label: string;
  to: string;
};

const REQUEST_ID_PATTERN = /^#?\d+$/;

export function GlobalSearch({ sidebarItems }: { sidebarItems: LocalizedShellSidebarItem[] }) {
  const { messages } = useLocale();
  const copy = messages.shell;
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== "k" || !(event.metaKey || event.ctrlKey)) return;
      event.preventDefault();
      setOpen((current) => {
        if (current) {
          setQuery("");
          setActiveIndex(0);
        }
        return !current;
      });
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  function setOpenState(next: boolean) {
    setOpen(next);
    if (!next) {
      setQuery("");
      setActiveIndex(0);
    }
  }

  // 输入模型名却得到「没有匹配的页面」是这个搜索框最常见的落空。
  // 打开时懒加载一次模型配置列表，读失败就明说，不静默缺组。
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const [modelsFailed, setModelsFailed] = useState(false);
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void getSharedModels(0)
      .then((loaded) => {
        if (cancelled) return;
        setModels(loaded);
        setModelsFailed(false);
      })
      .catch(() => {
        if (!cancelled) setModelsFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  const entries = useMemo<Entry[]>(() => {
    const trimmed = query.trim();
    const needle = trimmed.toLowerCase();
    const groupOrder = new Map(SHELL_SIDEBAR_GROUP_ORDER.map((id, index) => [id, index]));
    const pages = sidebarItems
      .filter((item) => !trimmed || item.label.toLowerCase().includes(needle))
      .sort((left, right) => (groupOrder.get(left.groupId) ?? 0) - (groupOrder.get(right.groupId) ?? 0))
      .map<Entry>((item) => ({
        description: messages.shell.groupLabels[item.groupId],
        icon: item.icon,
        key: `page:${item.id}`,
        label: item.label,
        to: item.to,
      }));

    const modelEntries = trimmed
      ? models
          .filter(
            (model) =>
              model.model_id.toLowerCase().includes(needle) ||
              (model.display_name ?? "").toLowerCase().includes(needle),
          )
          .slice(0, 8)
          .map<Entry>((model) => ({
            description: model.model_id,
            icon: CornerDownLeft,
            key: `model:${model.id}`,
            label: model.display_name?.trim() || model.model_id,
            to: `/route/models/${model.id}`,
          }))
      : [];

    if (!REQUEST_ID_PATTERN.test(trimmed)) return [...modelEntries, ...pages];

    const requestId = trimmed.replace(/^#/, "");
    return [
      ...modelEntries,
      {
        icon: CornerDownLeft,
        key: `request:${requestId}`,
        label: copy.searchJumpToRequest(requestId),
        to: `/observe/requests?request_id=${requestId}`,
      },
      {
        icon: CornerDownLeft,
        key: `audit:${requestId}`,
        label: copy.searchJumpToRequestAudit(requestId),
        to: `/observe/requests/${requestId}/audit`,
      },
      ...pages,
    ];
  }, [copy, messages.shell.groupLabels, models, query, sidebarItems]);

  const boundedIndex = entries.length === 0 ? 0 : Math.min(activeIndex, entries.length - 1);

  function go(entry: Entry | undefined) {
    if (!entry) return;
    setOpenState(false);
    void navigate({ to: entry.to });
  }

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => setOpenState(true)}
        data-testid="shell-global-search-trigger"
        aria-label={copy.search}
        className="h-[var(--density-control-h-sm)] justify-start gap-2 rounded-md px-2 text-muted-foreground max-sm:w-[var(--density-control-h-sm)] max-sm:justify-center max-sm:px-0 sm:w-56"
      >
        <Search aria-hidden="true" className="size-4" />
        {/* 窄屏上这个 224px 的按钮会把面包屑挤到 0–2px：收成一个图标钮。 */}
        <span className="truncate text-xs max-sm:hidden">{copy.search}</span>
        <kbd className="ml-auto rounded-[4px] border border-border bg-inset px-1 font-mono text-[10px] text-muted-foreground max-sm:hidden">
          {copy.searchShortcutHint}
        </kbd>
      </Button>

      <Dialog open={open} onOpenChange={setOpenState}>
        <DialogContent
          className="top-24 max-w-[36rem] translate-y-0 gap-0 p-0"
          data-testid="shell-global-search"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            inputRef.current?.focus();
          }}
        >
          <DialogHeader className="sr-only">
            <DialogTitle>{copy.searchDialogTitle}</DialogTitle>
            <DialogDescription>{copy.searchDialogDescription}</DialogDescription>
          </DialogHeader>

          <div className="flex items-center gap-2 border-b px-3">
            <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            <Input
              ref={inputRef}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setActiveIndex(0);
              }}
              onKeyDown={(event) => {
                if (event.key === "ArrowDown") {
                  event.preventDefault();
                  setActiveIndex((current) => (entries.length ? (current + 1) % entries.length : 0));
                } else if (event.key === "ArrowUp") {
                  event.preventDefault();
                  setActiveIndex((current) =>
                    entries.length ? (current - 1 + entries.length) % entries.length : 0,
                  );
                } else if (event.key === "Enter") {
                  event.preventDefault();
                  go(entries[boundedIndex]);
                }
              }}
              placeholder={copy.searchPlaceholder}
              aria-label={copy.searchDialogTitle}
              className="h-11 border-0 bg-transparent px-0 text-sm shadow-none focus-visible:border-0"
            />
          </div>

          <div className="max-h-80 overflow-y-auto p-2">
            {/* 读失败不能装成「没有模型配置」：那会让人以为搜索是全的。 */}
            {modelsFailed ? (
              <p className="px-2 pb-1 text-xs text-muted-foreground">
                {copy.searchModelsUnavailable}
              </p>
            ) : null}
            {entries.length === 0 ? (
              <p className="px-2 py-6 text-center text-xs text-muted-foreground">
                {copy.searchNoResults}
              </p>
            ) : (
              <ul className="flex flex-col gap-0.5">
                {entries.map((entry, index) => {
                  const EntryIcon = entry.icon;
                  return (
                    <li key={entry.key}>
                      <button
                        type="button"
                        onClick={() => go(entry)}
                        onMouseEnter={() => setActiveIndex(index)}
                        data-active={index === boundedIndex}
                        className={cn(
                          "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[0.8125rem]",
                          index === boundedIndex
                            ? "bg-primary-soft text-on-primary-soft"
                            : "text-foreground",
                        )}
                      >
                        <EntryIcon aria-hidden="true" className="size-4 shrink-0" />
                        <span className="truncate">{entry.label}</span>
                        {entry.description ? (
                          <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                            {entry.description}
                          </span>
                        ) : null}
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
