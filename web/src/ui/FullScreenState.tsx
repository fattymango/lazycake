import type { ReactNode } from "react";
import { Logo } from "./Logo";
import { Spinner } from "./Spinner";
import { ErrorState } from "./EmptyState";

/** Whole-window loading screen (first session check). */
export function FullScreenLoader() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-5 bg-bg">
      <Logo />
      <Spinner />
    </div>
  );
}

/** Whole-window failure that isn't "signed out": the server can't be reached. */
export function FullScreenError({ title, message, onRetry }: { title: ReactNode; message: ReactNode; onRetry: () => void }) {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-2 bg-bg p-6">
      <Logo className="mb-6" />
      <ErrorState title={title} message={message} onRetry={onRetry} />
    </div>
  );
}
