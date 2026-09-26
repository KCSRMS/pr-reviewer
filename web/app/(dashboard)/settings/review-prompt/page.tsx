"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useToken } from "@/hooks/useToken";
import { getReviewPrompt, putReviewPrompt } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";

export default function ReviewPromptPage() {
  const { token } = useToken();
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [isDefault, setIsDefault] = useState(true);
  const [loadError, setLoadError] = useState(false);

  useEffect(() => {
    if (!token) return;
    getReviewPrompt(token)
      .then((s) => {
        setPrompt(s.prompt ?? "");
        setIsDefault(s.is_default);
      })
      .catch(() => {
        setLoadError(true);
        toast.error("Failed to load review prompt");
      })
      .finally(() => setLoading(false));
  }, [token]);

  async function save(next: string) {
    if (!token) return;
    setSaving(true);
    try {
      const s = await putReviewPrompt(token, next);
      setPrompt(s.prompt ?? "");
      setIsDefault(s.is_default);
      toast.success(s.is_default ? "Using each agent's built-in prompt" : "Review prompt saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  if (loading) {
    return (
      <div className="space-y-4 max-w-4xl">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-96 w-full" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="space-y-4 max-w-4xl">
        <Button variant="ghost" size="sm" className="-ml-2 text-muted-foreground" onClick={() => router.back()}>← Back</Button>
        <p>Failed to load review prompt.</p>
      </div>
    );
  }

  return (
    <div className="space-y-8 max-w-4xl">
      <div>
        <Button variant="ghost" size="sm" className="-ml-2 mb-3 text-muted-foreground" onClick={() => router.back()}>← Back</Button>
        <h1 className="text-3xl font-bold">Review prompt</h1>
        <p className="text-base text-muted-foreground mt-1">
          Leave this empty to keep each agent's built-in prompt. A saved prompt replaces those prompts, and the JSON output format is added automatically.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">System prompt</CardTitle>
          <CardDescription className="text-base">
            {isDefault
              ? "No custom prompt is saved. Code review, security, performance, and database each keep their built-in prompt."
              : "A custom prompt is saved for this installation and replaces the built-in agent prompts."}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="Optional. Empty uses the built-in agent prompts."
            className="min-h-[28rem] font-mono text-sm"
            spellCheck={false}
          />
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm text-muted-foreground">{prompt.length.toLocaleString()} characters</span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                disabled={saving || prompt.trim() === ""}
                onClick={() => save("")}
              >
                Reset to default
              </Button>
              <Button size="lg" disabled={saving || loadError} onClick={() => save(prompt)}>
                {saving ? "Saving…" : "Save prompt"}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
