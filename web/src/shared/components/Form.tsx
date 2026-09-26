import type { InputHTMLAttributes, ReactNode, TextareaHTMLAttributes } from "react";

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="block mb-4">
      <div className="text-xs font-medium text-muted mb-1.5">{label}</div>
      {children}
      {hint && <div className="text-xs text-muted mt-1.5">{hint}</div>}
    </label>
  );
}

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={`w-full bg-bg border border-border rounded-lg px-3 py-2 text-sm text-text placeholder:text-muted/70 focus:outline-none focus:ring-2 focus:ring-accent/30 focus:border-accent transition-colors ${props.className ?? ""}`}
    />
  );
}

export function TextArea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...props}
      className={`w-full bg-bg border border-border rounded-lg px-3 py-2 text-sm text-text font-mono placeholder:text-muted/70 focus:outline-none focus:ring-2 focus:ring-accent/30 focus:border-accent transition-colors ${props.className ?? ""}`}
    />
  );
}

export function Button({
  children,
  variant = "primary",
  size = "md",
  className = "",
  ...rest
}: {
  children: ReactNode;
  variant?: "primary" | "secondary" | "danger" | "ghost";
  size?: "md" | "sm";
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  const variants: Record<string, string> = {
    primary: "bg-accent text-bg font-medium hover:opacity-90 disabled:opacity-50",
    secondary: "border border-border text-text hover:border-accent disabled:opacity-50",
    danger: "border border-bad/40 text-bad hover:bg-bad/10 disabled:opacity-50",
    ghost: "text-muted hover:text-text disabled:opacity-50",
  };
  const sizes: Record<string, string> = {
    md: "px-4 py-2 text-sm",
    sm: "px-2.5 py-1.5 text-xs",
  };
  return (
    <button
      {...rest}
      className={`inline-flex items-center justify-center gap-1.5 rounded-lg transition-colors ${sizes[size]} ${variants[variant]} ${className}`}
    >
      {children}
    </button>
  );
}

export function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div className="text-bad text-sm mb-4 bg-bad/10 border border-bad/30 rounded-lg px-3 py-2">{message}</div>
  );
}
