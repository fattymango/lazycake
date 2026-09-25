import { useState } from "react";
import { apiPost, ApiError } from "@shared/api";
import { copyToClipboard } from "@shared/clipboard";
import { Panel } from "@shared/components/Panel";
import { Button, FormError } from "@shared/components/Form";
import type { InstallToken } from "@shared/types";

export function AddMachine() {
  const [result, setResult] = useState<InstallToken | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [minting, setMinting] = useState(false);
  const [copied, setCopied] = useState(false);

  async function handleMint() {
    setError(null);
    setMinting(true);
    try {
      setResult(await apiPost<InstallToken>("/api/portal/provider/nodes/install-token"));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to mint an install token");
    } finally {
      setMinting(false);
    }
  }

  async function handleCopy() {
    if (!result) return;
    const ok = await copyToClipboard(result.install_command);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } else {
      setError("couldn't copy automatically — select the command above and copy it manually");
    }
  }

  return (
    <div className="max-w-2xl flex flex-col gap-6">
      <h1 className="text-xl font-semibold">Add a machine</h1>

      <Panel>
        <p className="text-sm text-muted mb-4">
          Minting an install token gives you a one-time command to run on the machine you want
          to lend out. The token is shown once — copy it before leaving this page.
        </p>
        <FormError message={error} />
        {!result ? (
          <Button onClick={handleMint} disabled={minting}>
            {minting ? "minting…" : "Mint install token"}
          </Button>
        ) : (
          <div className="flex flex-col gap-3">
            <div className="text-xs text-warn">
              This token will not be shown again. Copy it now.
            </div>
            <pre className="bg-black/40 border border-border rounded p-3 font-mono text-xs overflow-x-auto whitespace-pre-wrap break-all">
              {result.install_command}
            </pre>
            <div>
              <Button variant="secondary" onClick={handleCopy}>
                {copied ? "copied!" : "copy to clipboard"}
              </Button>
            </div>
          </div>
        )}
      </Panel>
    </div>
  );
}
