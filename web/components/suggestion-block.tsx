import { Lightbulb } from "lucide-react";

export function SuggestionBlock({
  suggestion,
  line,
  startLine,
}: {
  suggestion: string;
  line: number;
  startLine?: number;
}) {
  const rangeLabel = startLine && startLine < line ? `lines ${startLine}–${line}` : `line ${line}`;

  return (
    <div className="mt-2 rounded-md border border-green-300/50 dark:border-green-700/40 overflow-hidden">
      <div className="flex items-center gap-1.5 px-3 py-1 text-xs font-medium bg-green-50 dark:bg-green-950/30 text-green-700 dark:text-green-400 border-b border-green-300/50 dark:border-green-700/40">
        <Lightbulb className="h-3 w-3" aria-hidden="true" />
        Suggested fix &middot; {rangeLabel}
      </div>
      <pre className="px-3 py-2 text-xs font-mono whitespace-pre-wrap bg-muted/30 overflow-x-auto">{suggestion}</pre>
    </div>
  );
}
