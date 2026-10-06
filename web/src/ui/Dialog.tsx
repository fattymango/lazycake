import * as RadixDialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";
import { X } from "lucide-react";
import { cn } from "@/lib/cn";
import { Button } from "./Button";

export const Dialog = RadixDialog.Root;
export const DialogTrigger = RadixDialog.Trigger;
export const DialogClose = RadixDialog.Close;

export function DialogContent({
  title,
  description,
  children,
  footer,
  className,
  hideClose,
}: {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  className?: string;
  hideClose?: boolean;
}) {
  return (
    <RadixDialog.Portal>
      <RadixDialog.Overlay className="fixed inset-0 z-50 animate-fade-in bg-black/60 backdrop-blur-[2px]" />
      <RadixDialog.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-50 flex max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 animate-pop-in flex-col rounded-2xl border border-border bg-surface shadow-pop focus:outline-none",
          className
        )}
      >
        <div className="flex items-start justify-between gap-4 px-6 pb-2 pt-5">
          <div className="min-w-0">
            <RadixDialog.Title className="text-base font-semibold leading-6 text-fg">{title}</RadixDialog.Title>
            {description ? (
              <RadixDialog.Description className="mt-1 text-sm leading-5 text-muted">{description}</RadixDialog.Description>
            ) : (
              <RadixDialog.Description className="sr-only">{typeof title === "string" ? title : "Dialog"}</RadixDialog.Description>
            )}
          </div>
          {!hideClose && (
            <RadixDialog.Close asChild>
              <Button variant="ghost" size="icon-sm" aria-label="Close" className="-mr-2 -mt-1">
                <X />
              </Button>
            </RadixDialog.Close>
          )}
        </div>
        {children && <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3">{children}</div>}
        {footer && <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border px-6 py-4">{footer}</div>}
      </RadixDialog.Content>
    </RadixDialog.Portal>
  );
}

/** A yes/no question. `danger` styles the confirm button as destructive. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  danger,
  busy,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent
        title={title}
        description={description}
        className="max-w-md"
        footer={
          <>
            <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant={danger ? "danger-solid" : "primary"} onClick={onConfirm} loading={busy}>
              {confirmLabel}
            </Button>
          </>
        }
      />
    </Dialog>
  );
}
