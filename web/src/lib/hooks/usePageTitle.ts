import { useEffect } from "react";

/** Sets the browser tab title: "Tasks · LazyCake". */
export function usePageTitle(title: string | undefined) {
  useEffect(() => {
    document.title = title ? `${title} · LazyCake` : "LazyCake";
  }, [title]);
}
