import { NavLink } from "react-router-dom";
import type { ReactNode } from "react";

type NavItem = {
  to: string;
  label: string;
  icon: ReactNode;
};

const iconProps = {
  width: 20,
  height: 20,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.75,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

const items: NavItem[] = [
  {
    to: "/",
    label: "Dashboard",
    icon: (
      <svg {...iconProps}>
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
    ),
  },
  {
    to: "/sessions",
    label: "Sessions",
    icon: (
      <svg {...iconProps}>
        <rect x="3" y="4" width="18" height="16" rx="2" />
        <path d="M3 9h18M8 4v0" />
      </svg>
    ),
  },
  {
    to: "/waterfall",
    label: "Waterfall",
    icon: (
      <svg {...iconProps}>
        <path d="M4 20 L9 9 L13 15 L20 4" />
      </svg>
    ),
  },
  {
    to: "/efficiency",
    label: "Efficiency",
    icon: (
      <svg {...iconProps}>
        <path d="M4 15a8 8 0 0 1 16 0" />
        <path d="M12 15l4-6" />
        <circle cx="12" cy="15" r="1" />
      </svg>
    ),
  },
  {
    to: "/waste",
    label: "Waste",
    icon: (
      <svg {...iconProps}>
        <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
      </svg>
    ),
  },
  {
    to: "/tools",
    label: "Tools & MCP",
    icon: (
      <svg {...iconProps}>
        <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.6 2.6-2-2z" />
      </svg>
    ),
  },
  {
    to: "/search",
    label: "Search",
    icon: (
      <svg {...iconProps}>
        <circle cx="11" cy="11" r="7" />
        <path d="M21 21l-4.3-4.3" />
      </svg>
    ),
  },
];

export function NavRail() {
  return (
    <nav className="flex h-full w-20 shrink-0 flex-col items-stretch gap-1 border-r border-wire bg-panel py-4">
      <div className="mb-4 flex justify-center">
        <span className="font-display text-sm font-semibold tracking-wide text-signal">
          cc
        </span>
      </div>
      {items.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.to === "/"}
          className={({ isActive }) =>
            [
              "flex flex-col items-center gap-1 px-1 py-2.5 text-[11px] font-medium transition-colors",
              isActive
                ? "text-signal"
                : "text-ink/60 hover:text-ink",
            ].join(" ")
          }
        >
          {item.icon}
          <span className="leading-tight text-center">{item.label}</span>
        </NavLink>
      ))}
    </nav>
  );
}
