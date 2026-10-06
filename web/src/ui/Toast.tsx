import { createContext, useCallback, useContext, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from "lucide-react";
import { cn } from "@/lib/cn";
import { toneClasses, type Tone } from "./tone";

interface ToastItem {
  id: number;
  title: string;
  description?: string;
  tone: Tone;
}

interface ToastApi {
  toast: (t: { title: string; description?: string; tone?: Tone; durationMs?: number }) => void;
  success: (title: string, description?: string) => void;
  error: (title: string, description?: string) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

const icons: Record<Tone, typeof Info> = {
  neutral: Info,
  accent: Info,
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: XCircle,
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => setItems((cur) => cur.filter((t) => t.id !== id)), []);

  const toast = useCallback<ToastApi["toast"]>(
    ({ title, description, tone = "neutral", durationMs = 4500 }) => {
      const id = nextId.current++;
      setItems((cur) => [...cur.slice(-3), { id, title, description, tone }]);
      window.setTimeout(() => dismiss(id), durationMs);
    },
    [dismiss]
  );

  const api = useMemo<ToastApi>(
    () => ({
      toast,
      success: (title, description) => toast({ title, description, tone: "success" }),
      error: (title, description) => toast({ title, description, tone: "danger", durationMs: 7000 }),
    }),
    [toast]
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-[70] flex flex-col items-center gap-2 p-4 sm:items-end sm:p-6"
      >
        {items.map((t) => {
          const Icon = icons[t.tone];
          return (
            <div
              key={t.id}
              role="status"
              className="pointer-events-auto flex w-full max-w-sm animate-slide-up items-start gap-3 rounded-xl border border-border bg-overlay p-3.5 shadow-pop"
            >
              <Icon className={cn("mt-0.5 size-4 shrink-0", t.tone === "neutral" ? "text-muted" : toneClasses[t.tone].text)} aria-hidden />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium leading-5 text-fg">{t.title}</p>
                {t.description && <p className="mt-0.5 break-words text-xs leading-5 text-muted">{t.description}</p>}
              </div>
              <button
                onClick={() => dismiss(t.id)}
                aria-label="Dismiss"
                className="-m-1 rounded-md p-1 text-muted transition-colors hover:bg-raised hover:text-fg"
              >
                <X className="size-3.5" />
              </button>
            </div>
          );
        })}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
