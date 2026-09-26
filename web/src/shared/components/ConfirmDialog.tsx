import { Button } from "./Form";
import { IconAlertTriangle } from "./Icon";

// A real confirmation dialog for destructive actions (deleting/stopping a
// machine) - a bare window.confirm() works but looks exactly like the
// browser chrome it's borrowed from, not part of the product.
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "Confirm",
  danger = true,
  busy = false,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description: string;
  confirmLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/60" onClick={onCancel} />
      <div className="relative bg-panel border border-border rounded-xl2 shadow-popover max-w-sm w-full p-5">
        <div className="flex items-start gap-3 mb-2">
          <div
            className={`w-9 h-9 rounded-lg flex items-center justify-center shrink-0 ${
              danger ? "bg-bad/10 border border-bad/30 text-bad" : "bg-panel2 border border-border text-muted"
            }`}
          >
            <IconAlertTriangle className="w-4.5 h-4.5" />
          </div>
          <div className="min-w-0">
            <h3 className="font-semibold text-sm">{title}</h3>
            <p className="text-sm text-muted mt-1">{description}</p>
          </div>
        </div>
        <div className="flex justify-end gap-2 mt-5">
          <Button variant="secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant={danger ? "danger" : "primary"} onClick={onConfirm} disabled={busy}>
            {busy ? "Working…" : confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
