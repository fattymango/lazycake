import type { SVGProps } from "react";

// A small hand-rolled icon set, not an added dependency - every icon this
// UI actually needs, nothing more. All 20x20, stroke-based, inherit
// currentColor so they pick up whatever text color their container sets.
type IconProps = SVGProps<SVGSVGElement>;

function base(props: IconProps) {
  return {
    width: 20,
    height: 20,
    viewBox: "0 0 20 20",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.6,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    ...props,
  };
}

export function IconDashboard(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect x="2.5" y="2.5" width="6" height="6" rx="1.2" />
      <rect x="11.5" y="2.5" width="6" height="6" rx="1.2" />
      <rect x="2.5" y="11.5" width="6" height="6" rx="1.2" />
      <rect x="11.5" y="11.5" width="6" height="6" rx="1.2" />
    </svg>
  );
}

export function IconPlay(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M5 3.5v13l11-6.5-11-6.5z" fill="currentColor" stroke="none" />
    </svg>
  );
}

export function IconHistory(props: IconProps) {
  return (
    <svg {...base(props)}>
      <circle cx="10" cy="10" r="7.2" />
      <path d="M10 5.8V10l3 2" />
    </svg>
  );
}

export function IconGateway(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect x="3" y="7" width="14" height="9" rx="1.4" />
      <path d="M6.5 7V5.2A3.5 3.5 0 0 1 10 1.7a3.5 3.5 0 0 1 3.5 3.5V7" />
      <circle cx="10" cy="11.5" r="1.2" fill="currentColor" stroke="none" />
    </svg>
  );
}

export function IconBilling(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect x="2.5" y="4.5" width="15" height="11" rx="1.6" />
      <path d="M2.5 8.2h15" />
      <path d="M5.5 12h3" />
    </svg>
  );
}

export function IconMachine(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect x="2.5" y="3.5" width="15" height="9.5" rx="1.4" />
      <path d="M7 17h6M10 13v4" />
    </svg>
  );
}

export function IconPlus(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M10 4v12M4 10h12" />
    </svg>
  );
}

export function IconEarnings(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M3 15l4.5-5 3.5 3 6-7" />
      <path d="M13 6h3.5v3.5" />
    </svg>
  );
}

export function IconLogout(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M8 3.5H4.6A1.1 1.1 0 0 0 3.5 4.6v10.8a1.1 1.1 0 0 0 1.1 1.1H8" />
      <path d="M13 13.5l4-3.5-4-3.5M17 10H8" />
    </svg>
  );
}

export function IconChevronDown(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M5 7.5l5 5 5-5" />
    </svg>
  );
}

export function IconTrash(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M4 6h12M8 6V4.3a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1V6M6 6l.7 9.3a1.4 1.4 0 0 0 1.4 1.2h3.8a1.4 1.4 0 0 0 1.4-1.2L14 6" />
      <path d="M8.3 9v4.5M11.7 9v4.5" />
    </svg>
  );
}

export function IconStop(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect x="5" y="5" width="10" height="10" rx="1.6" fill="currentColor" stroke="none" />
    </svg>
  );
}

export function IconBox(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M10 2.5l7 3.6v7.8L10 17.5l-7-3.6V6.1L10 2.5z" />
      <path d="M3 6.1L10 9.7l7-3.6M10 9.7v7.8" />
    </svg>
  );
}

export function IconAlertTriangle(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M10 3.2l8 13.6H2l8-13.6z" />
      <path d="M10 8v4M10 14.5v.1" />
    </svg>
  );
}

export function IconCopy(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect x="7.5" y="7.5" width="9" height="9" rx="1.2" />
      <path d="M13 7.5V4.6a1.1 1.1 0 0 0-1.1-1.1H4.6a1.1 1.1 0 0 0-1.1 1.1v7.3a1.1 1.1 0 0 0 1.1 1.1H7.5" />
    </svg>
  );
}

export function IconCheck(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M4 10.5l4 4 8-9" />
    </svg>
  );
}

export function IconInbox(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M3 11l2.2-6.3A1.2 1.2 0 0 1 6.3 4h7.4a1.2 1.2 0 0 1 1.1.7L17 11" />
      <path d="M3 11v4a1.2 1.2 0 0 0 1.2 1.2h11.6A1.2 1.2 0 0 0 17 15v-4h-3.6a1 1 0 0 0-.9.55l-.4.9a1 1 0 0 1-.9.55H8.8a1 1 0 0 1-.9-.55l-.4-.9a1 1 0 0 0-.9-.55H3z" />
    </svg>
  );
}
