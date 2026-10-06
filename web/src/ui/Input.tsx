import { cloneElement, forwardRef, isValidElement, useId } from "react";
import type { InputHTMLAttributes, ReactElement, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/cn";

const control =
  "w-full rounded-lg border border-border bg-surface text-sm text-fg shadow-sm transition-[border-color,box-shadow] duration-150 placeholder:text-subtle hover:border-border-strong focus:border-accent/70 focus:outline-none focus:ring-4 focus:ring-accent/15 disabled:cursor-not-allowed disabled:opacity-60 aria-[invalid=true]:border-danger/60 aria-[invalid=true]:focus:ring-danger/15";

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  /** Icon or text shown inside the field's left edge. */
  leading?: ReactNode;
  /** Element shown inside the right edge (e.g. a show-password button). */
  trailing?: ReactNode;
  mono?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ className, leading, trailing, mono, ...props }, ref) {
  if (!leading && !trailing) {
    return <input ref={ref} className={cn(control, "h-9 px-3", mono && "font-mono text-[13px]", className)} {...props} />;
  }
  return (
    <div className={cn("relative", className)}>
      {leading && (
        <span className="pointer-events-none absolute inset-y-0 left-3 flex items-center text-muted [&_svg]:size-4">{leading}</span>
      )}
      <input
        ref={ref}
        className={cn(control, "h-9", leading ? "pl-9" : "pl-3", trailing ? "pr-10" : "pr-3", mono && "font-mono text-[13px]")}
        {...props}
      />
      {trailing && <span className="absolute inset-y-0 right-1.5 flex items-center">{trailing}</span>}
    </div>
  );
});

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement> & { mono?: boolean }>(function Textarea(
  { className, mono, ...props },
  ref
) {
  return <textarea ref={ref} className={cn(control, "min-h-[84px] px-3 py-2", mono && "font-mono text-[13px]", className)} {...props} />;
});

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function Select(
  { className, children, ...props },
  ref
) {
  return (
    <div className={cn("relative", className)}>
      <select ref={ref} className={cn(control, "h-9 appearance-none pl-3 pr-9")} {...props}>
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted" aria-hidden />
    </div>
  );
});

/**
 * Label + control + hint/error. Wires the label, hint and error to the
 * control for screen readers: pass a single control as the child.
 */
export function Field({
  label,
  hint,
  error,
  required,
  children,
  className,
  labelAction,
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: string | null;
  required?: boolean;
  children: ReactElement;
  className?: string;
  labelAction?: ReactNode;
}) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errId = `${id}-err`;
  const described = [hint ? hintId : "", error ? errId : ""].filter(Boolean).join(" ") || undefined;
  const control = isValidElement(children)
    ? cloneElement(children as ReactElement<Record<string, unknown>>, {
        id,
        "aria-describedby": described,
        "aria-invalid": error ? true : undefined,
        required: required || undefined,
      })
    : children;
  return (
    <div className={cn("space-y-1.5", className)}>
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={id} className="text-xs font-medium text-fg">
          {label}
          {required && (
            <span className="ml-0.5 text-danger" aria-hidden>
              *
            </span>
          )}
        </label>
        {labelAction}
      </div>
      {control}
      {hint && !error && (
        <p id={hintId} className="text-xs leading-5 text-muted">
          {hint}
        </p>
      )}
      {error && (
        <p id={errId} role="alert" className="text-xs leading-5 text-danger">
          {error}
        </p>
      )}
    </div>
  );
}
