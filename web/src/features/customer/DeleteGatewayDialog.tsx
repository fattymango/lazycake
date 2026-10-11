import { useState } from "react";
import { apiDelete, errorMessage } from "@/lib/api";
import { GATEWAY_REMOVE_COMMAND } from "@/lib/gatewayInstall";
import type { Gateway } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { CodeBlock } from "@/ui/CodeBlock";
import { Dialog, DialogContent } from "@/ui/Dialog";
import { useToast } from "@/ui/Toast";

/**
 * Deleting a gateway: confirm, then show how to remove the container from its machine. `onClosed` runs when the
 * dialog closes after a successful delete, so the page only drops the gateway once the owner has seen the command
 * (a page that unmounted the dialog the moment the list reloaded would swallow it).
 */
export function DeleteGatewayDialog({
  gateway,
  onClose,
  onClosed,
}: {
  gateway: Pick<Gateway, "id" | "label" | "connected"> | null;
  onClose: () => void;
  onClosed: () => void;
}) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const close = () => {
    const wasDone = done;
    onClose();
    window.setTimeout(() => {
      setError(null);
      setDone(false);
    }, 200);
    if (wasDone) onClosed();
  };

  async function remove() {
    if (!gateway) return;
    setBusy(true);
    setError(null);
    try {
      await apiDelete(`/api/portal/customer/gateways/${encodeURIComponent(gateway.id)}`);
      setDone(true);
      toast.success("Gateway deleted", gateway.label);
    } catch (err) {
      setError(errorMessage(err, "Couldn't delete the gateway"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={gateway !== null} onOpenChange={(o) => !o && close()}>
      {gateway && !done && (
        <DialogContent
          title={`Delete ${gateway.label}?`}
          description="Tasks will no longer be able to reach your services through it."
          footer={
            <>
              <Button variant="secondary" type="button" onClick={close}>
                Cancel
              </Button>
              <Button variant="danger-solid" type="button" loading={busy} onClick={remove}>
                Delete gateway
              </Button>
            </>
          }
        >
          <div className="space-y-3 pb-2">
            {error && <Alert tone="danger">{error}</Alert>}
            <ul className="list-disc space-y-1.5 pl-5 text-sm leading-6 text-muted">
              <li>Its token expires immediately. Starting the container again is refused.</li>
              {gateway.connected && <li>It is connected right now: the connection is dropped and the gateway stops serving.</li>}
              <li>It leaves your list. Past traffic stays in the history of the tasks that used it.</li>
              <li>This can't be undone. To publish the same services again, create a new gateway.</li>
            </ul>
          </div>
        </DialogContent>
      )}
      {gateway && done && (
        <DialogContent
          title="Gateway deleted"
          description="It can no longer connect. Remove it from its machine to finish the clean-up."
          footer={<Button onClick={close}>Done</Button>}
        >
          <div className="space-y-3 pb-2">
            <p className="text-sm leading-6 text-muted">
              A connected gateway stops by itself and stays idle. Because LazyCake never has access to your machine, run this there to
              delete the container and its key:
            </p>
            <CodeBlock label="Shell" code={GATEWAY_REMOVE_COMMAND} />
          </div>
        </DialogContent>
      )}
    </Dialog>
  );
}
