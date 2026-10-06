import * as Menu from "@radix-ui/react-dropdown-menu";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "@/lib/cn";

export const DropdownMenu = Menu.Root;
export const DropdownMenuTrigger = Menu.Trigger;

export function DropdownMenuContent({ className, children, align = "end", ...props }: ComponentPropsWithoutRef<typeof Menu.Content>) {
  return (
    <Menu.Portal>
      <Menu.Content
        align={align}
        sideOffset={8}
        collisionPadding={12}
        className={cn("z-50 min-w-[12rem] animate-pop-in rounded-xl border border-border bg-overlay p-1.5 shadow-pop", className)}
        {...props}
      >
        {children}
      </Menu.Content>
    </Menu.Portal>
  );
}

export function DropdownMenuItem({
  className,
  icon,
  danger,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof Menu.Item> & { icon?: ReactNode; danger?: boolean }) {
  return (
    <Menu.Item
      className={cn(
        "flex cursor-pointer select-none items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm outline-none transition-colors data-[disabled]:pointer-events-none data-[highlighted]:bg-raised data-[disabled]:opacity-50 [&_svg]:size-4 [&_svg]:text-muted",
        danger ? "text-danger [&_svg]:text-danger" : "text-fg",
        className
      )}
      {...props}
    >
      {icon}
      {children}
    </Menu.Item>
  );
}

export const DropdownMenuSeparator = ({ className }: { className?: string }) => (
  <Menu.Separator className={cn("my-1.5 h-px bg-border", className)} />
);

export function DropdownMenuLabel({ className, ...props }: ComponentPropsWithoutRef<typeof Menu.Label>) {
  return <Menu.Label className={cn("px-2.5 py-1.5 text-2xs font-medium uppercase tracking-wider text-muted", className)} {...props} />;
}
