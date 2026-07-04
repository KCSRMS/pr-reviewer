"use client";

import { useState } from "react";
import { Lightbulb, CheckCircle2 } from "lucide-react";

export function SuggestionBlock({
  suggestion,
  line,
  startLine,
  applied,
  appliedBy,
  onApply,
}: {
  suggestion: string;
  line: number;
  startLine?: number;
  applied?: boolean;
  appliedBy?: string;
  onApply?: () => Promise<void>;
}) {
  const [applying, setApplying] = useState(false);
  const rangeLabel = startLine && startLine < line ? `lines ${startLine}–${line}` : `line ${line}`;

  async function handleApply() {
    if (!onApply) return;
    setApplying(true);
    try {
      await onApply();
    } finally {
      setApplying(false);
    }
  }

  return (
    <div className="mt-2 rounded-md border border-green-300/50 dark:border-green-700/40 overflow-hidden">
      <div className="flex items-center justify-between gap-2 px-3 py-1 text-xs font-medium bg-green-50 dark:bg-green-950/30 text-green-700 dark:text-green-400 border-b border-green-300/50 dark:border-green-700/40">
        <span className="flex items-center gap-1.5">
          <Lightbulb className="h-3 w-3" aria-hidden="true" />
          Suggested fix &middot; {rangeLabel}
        </span>
        {applied ? (
          <span className="flex items-center gap-1 text-muted-foreground">
            <CheckCircle2 className="h-3 w-3" aria-hidden="true" />
            Applied{appliedBy ? ` by ${appliedBy}` : ""}
          </span>
        ) : (
          onApply && (
            <button
              type="button"
              onClick={handleApply}
              disabled={applying}
              className="rounded border border-green-400/60 dark:border-green-600/60 px-2 py-0.5 text-xs font-medium text-green-700 dark:text-green-400 hover:bg-green-100 dark:hover:bg-green-900/40 transition-colors disabled:opacity-50 cursor-pointer"
            >
              {applying ? "Applying…" : "Apply fix"}
            </button>
          )
        )}
      </div>
      <pre className="px-3 py-2 text-xs font-mono whitespace-pre-wrap bg-muted/30 overflow-x-auto">{suggestion}</pre>
    </div>
  );
}
