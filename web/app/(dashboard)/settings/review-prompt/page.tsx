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
  const [defaultPrompt, setDefaultPrompt] = useState("");
  const [isDefault, setIsDefault] = useState(true);

  useEffect(() => {
    if (!token) return;
    getReviewPrompt(token)
      .then((s) => {
        setPrompt(s.prompt);
        setDefaultPrompt(s.default_prompt);
        setIsDefault(s.is_default);
      })
      .catch(() => toast.error("Failed to load review prompt"))
      .finally(() => setLoading(false));
  }, [token]);

  async function save(next: string) {
    if (!token) return;
    setSaving(true);
    try {
      const s = await putReviewPrompt(token, next);
      setPrompt(s.prompt);
      setDefaultPrompt(s.default_prompt);
      setIsDefault(s.is_default);
      toast.success(s.is_default ? "Using the built-in review prompt" : "Review prompt saved");
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

  return (
    <div className="space-y-8 max-w-4xl">
      <div>
        <Button variant="ghost" size="sm" className="-ml-2 mb-3 text-muted-foreground" onClick={() => router.back()}>← Back</Button>
        <h1 className="text-3xl font-bold">Review prompt</h1>
        <p className="text-base text-muted-foreground mt-1">
          This policy is sent with every review. The JSON output format is added automatically and stays out of this text.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">System prompt</CardTitle>
          <CardDescription className="text-base">
            {isDefault
              ? "Using the built-in KCS review policy. Saving the same text keeps the built-in default."
              : "Using a custom policy stored for this installation."}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            className="min-h-[28rem] font-mono text-sm"
            spellCheck={false}
          />
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm text-muted-foreground">{prompt.length.toLocaleString()} characters</span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                disabled={saving || prompt === defaultPrompt}
                onClick={() => save(defaultPrompt)}
              >
                Reset to default
              </Button>
              <Button size="lg" disabled={saving} onClick={() => save(prompt)}>
                {saving ? "Saving…" : "Save prompt"}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
