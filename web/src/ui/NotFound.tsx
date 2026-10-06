import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { Button } from "./Button";

/** Used for unknown routes and for a resource (task, machine) that doesn't exist or isn't yours. */
export function NotFound({
  title = "Page not found",
  description = "The page you're looking for doesn't exist or has moved.",
  backTo = "/",
  backLabel = "Back to overview",
}: {
  title?: ReactNode;
  description?: ReactNode;
  backTo?: string;
  backLabel?: string;
}) {
  return (
    <div className="flex flex-col items-center justify-center px-4 py-20 text-center">
      <p className="bg-gradient-to-b from-fg to-fg/30 bg-clip-text text-7xl font-semibold tracking-tighter text-transparent" aria-hidden>
        404
      </p>
      <h1 className="mt-4 text-lg font-semibold text-fg">{title}</h1>
      <p className="mt-2 max-w-sm text-sm leading-6 text-muted">{description}</p>
      <Button asChild className="mt-6" variant="secondary">
        <Link to={backTo}>
          <ArrowLeft />
          {backLabel}
        </Link>
      </Button>
    </div>
  );
}
