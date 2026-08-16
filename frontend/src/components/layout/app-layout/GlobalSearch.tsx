import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { CornerDownLeft, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
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

  const entries = useMemo<Entry[]>(() => {
    const trimmed = query.trim();
    const groupOrder = new Map(SHELL_SIDEBAR_GROUP_ORDER.map((id, index) => [id, index]));
    const pages = sidebarItems
      .filter((item) => !trimmed || item.label.includes(trimmed))
      .sort((left, right) => (groupOrder.get(left.groupId) ?? 0) - (groupOrder.get(right.groupId) ?? 0))
      .map<Entry>((item) => ({
        description: messages.shell.groupLabels[item.groupId],
        icon: item.icon,
        key: `page:${item.id}`,
        label: item.label,
        to: item.to,
      }));

    if (!REQUEST_ID_PATTERN.test(trimmed)) return pages;

    const requestId = trimmed.replace(/^#/, "");
    return [
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
  }, [copy, messages.shell.groupLabels, query, sidebarItems]);

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
        className="h-[var(--density-control-h-sm)] w-56 justify-start gap-2 rounded-md px-2 text-muted-foreground"
      >
        <Search aria-hidden="true" className="size-4" />
        <span className="truncate text-xs">{copy.search}</span>
        <kbd className="ml-auto rounded-[4px] border border-border bg-inset px-1 font-mono text-[10px] text-muted-foreground">
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
