"use client";

import { useState } from "react";
import { Button } from "@/components/ui";

// Shows new recovery codes once, with copy and download.
export function RecoveryCodes({ codes, email }: { codes: string[]; email: string }) {
  const [copied, setCopied] = useState(false);
  const text = `HR Workspace recovery codes for ${email}\nEach code works once, if you lose your phone.\n\n${codes.join("\n")}\n`;

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  function download() {
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "hr-workspace-recovery-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="flex flex-col gap-3">
      <ol className="grid grid-cols-2 gap-x-6 gap-y-1.5 rounded-xl border border-line bg-canvas px-5 py-4 font-mono text-[15px] tracking-wide" aria-label="Recovery codes">
        {codes.map((c) => <li key={c}>{c}</li>)}
      </ol>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={copy}>{copied ? "Copied" : "Copy"}</Button>
        <Button size="sm" onClick={download}>Download .txt</Button>
      </div>
    </div>
  );
}
