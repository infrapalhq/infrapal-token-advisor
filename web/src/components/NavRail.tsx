import { NavLink } from "react-router-dom";
import type { ReactNode } from "react";
import { useNavBadges } from "../lib/navBadges";
import type { Theme } from "../lib/theme";

type NavItem = {
  to: string;
  label: string;
  icon: ReactNode;
  badge?: number;
};

const iconProps = {
  width: 17,
  height: 17,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export function NavRail({
  theme,
  onToggleTheme,
}: {
  theme: Theme;
  onToggleTheme: () => void;
}) {
  const badges = useNavBadges();

  const items: NavItem[] = [
    {
      to: "/",
      label: "Overview",
      icon: (
        <svg {...iconProps}>
          <rect x="3" y="3" width="7" height="7" rx="1.5" />
          <rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" />
          <rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
      ),
    },
    {
      to: "/optimize",
      label: "Optimize",
      icon: (
        <svg {...iconProps}>
          <path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z" />
        </svg>
      ),
    },
    {
      to: "/ai-mode",
      label: "AI Mode",
      icon: (
        <svg {...iconProps}>
          <path d="M12 2l1.8 5.4L19 9l-5.2 1.6L12 16l-1.8-5.4L5 9l5.2-1.6L12 2z" />
        </svg>
      ),
    },
    {
      to: "/sessions",
      label: "Sessions",
      icon: (
        <svg {...iconProps}>
          <path d="M3 12h4l3-7 4 14 3-7h4" />
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
      badge: badges.waste,
    },
    {
      to: "/tools",
      label: "Tools & MCP",
      icon: (
        <svg {...iconProps}>
          <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.6 2.6-2-2z" />
        </svg>
      ),
      badge: badges.tools,
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

  return (
    <nav className="flex w-[236px] shrink-0 flex-col border-r border-wire bg-chrome p-3">
      <div className="px-2 pb-2.5 text-[11px] tracking-wide text-ink/35 uppercase">
        Dashboard
      </div>
      <div className="flex flex-col gap-0.5">
        {items.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === "/"}
            className={({ isActive }) =>
              [
                "flex items-center justify-between gap-2.5 rounded-md px-2.5 py-2 text-[13.5px] font-medium transition-colors",
                isActive
                  ? "bg-signal/15 text-ink shadow-[inset_2px_0_0_var(--color-signal)]"
                  : "text-ink/55 hover:bg-panel-2 hover:text-ink",
              ].join(" ")
            }
          >
            <span className="flex items-center gap-2.5">
              {item.icon}
              {item.label}
            </span>
            {Boolean(item.badge) && (
              <span className="rounded-full bg-fault/15 px-2 py-0.5 text-[11.5px] font-semibold text-fault">
                {item.badge}
              </span>
            )}
          </NavLink>
        ))}
      </div>
      <button
        type="button"
        onClick={onToggleTheme}
        className="mt-auto flex items-center gap-2.5 border-t border-wire px-2 pt-3 text-xs text-ink/45 hover:text-ink"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 3v2M12 19v2M5 12H3M21 12h-2" />
        </svg>
        {theme === "dark" ? "Light theme" : "Dark theme"}
      </button>
    </nav>
  );
}
