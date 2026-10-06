import { Component } from "react";
import type { ErrorInfo, ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { Button } from "./Button";

interface Props {
  children: ReactNode;
  /** Render a smaller, inline fallback (inside the page) instead of a full-screen one. */
  inline?: boolean;
}

/**
 * Catches a render error anywhere below it and shows a designed screen
 * instead of a blank page. Give it a `key` that changes on navigation so
 * moving to another page recovers.
 */
export class ErrorBoundary extends Component<Props, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("render error:", error, info.componentStack);
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div className={this.props.inline ? "py-16" : "flex min-h-screen items-center justify-center bg-bg p-6"}>
        <div className="mx-auto w-full max-w-md text-center" role="alert">
          <div className="mx-auto mb-5 flex size-12 items-center justify-center rounded-2xl border border-danger/25 bg-danger/10 text-danger">
            <AlertTriangle className="size-6" aria-hidden />
          </div>
          <h1 className="text-lg font-semibold text-fg">This page hit a snag</h1>
          <p className="mt-2 text-sm leading-6 text-muted">
            Something unexpected went wrong while showing this page. Your data is safe. Reloading usually fixes it.
          </p>
          <details className="mt-4 rounded-lg border border-border bg-raised/50 p-3 text-left">
            <summary className="cursor-pointer text-xs font-medium text-muted">Technical details</summary>
            <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words font-mono text-xs text-muted">{error.message}</pre>
          </details>
          <div className="mt-6 flex justify-center gap-2">
            <Button onClick={() => window.location.reload()}>Reload page</Button>
            <Button variant="secondary" onClick={() => (window.location.href = "/")}>
              Go to overview
            </Button>
          </div>
        </div>
      </div>
    );
  }
}
