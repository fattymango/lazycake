import type { InputHTMLAttributes, ReactNode, TextareaHTMLAttributes } from "react";

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block mb-4">
      <div className="text-xs text-muted mb-1">{label}</div>
      {children}
    </label>
  );
}

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={`w-full bg-bg border border-border rounded px-3 py-2 text-sm text-text focus:outline-none focus:border-accent ${props.className ?? ""}`}
    />
  );
}

export function TextArea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...props}
      className={`w-full bg-bg border border-border rounded px-3 py-2 text-sm text-text font-mono focus:outline-none focus:border-accent ${props.className ?? ""}`}
    />
  );
}

export function Button({
  children,
  variant = "primary",
  className = "",
  ...rest
}: { children: ReactNode; variant?: "primary" | "secondary" } & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  const styles =
    variant === "primary"
      ? "bg-accent text-bg font-medium hover:opacity-90 disabled:opacity-50"
      : "border border-border text-text hover:border-accent disabled:opacity-50";
  return (
    <button {...rest} className={`rounded px-4 py-2 text-sm transition-colors ${styles} ${className}`}>
      {children}
    </button>
  );
}

export function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return <div className="text-bad text-sm mb-4">{message}</div>;
}
